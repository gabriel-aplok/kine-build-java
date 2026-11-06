package voxel;

import com.badlogic.gdx.graphics.Color;
import com.badlogic.gdx.graphics.Mesh;
import com.badlogic.gdx.graphics.VertexAttribute;
import com.badlogic.gdx.graphics.VertexAttributes.Usage;
import com.badlogic.gdx.math.MathUtils;
import com.badlogic.gdx.utils.FloatArray;
import com.badlogic.gdx.utils.ShortArray;

import java.util.ArrayList;
import java.util.Comparator;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Random;

/** Editable heightmap terrain with trees and one static mesh for visible faces. */
public final class World {
    public static final byte AIR = 0;
    public static final byte GRASS = 1;
    public static final byte DIRT = 2;
    public static final byte STONE = 3;
    public static final byte WOOD = 4;
    public static final byte LEAVES = 5;
    public static final byte LAMP = 6;

    private static final int MAX_Y = 64;
    private static final int TOP = 0;
    private static final int BOTTOM = 1;
    private static final int NORTH = 2;
    private static final int SOUTH = 3;
    private static final int WEST = 4;
    private static final int EAST = 5;

    private static final int FLOATS_PER_VERTEX = 11;

    private static final Color GRASS_TOP = new Color(0.36f, 0.68f, 0.28f, 1f);
    private static final Color DIRT_COLOR = new Color(0.45f, 0.31f, 0.18f, 1f);
    private static final Color STONE_COLOR = new Color(0.44f, 0.44f, 0.47f, 1f);
    private static final Color WOOD_COLOR = new Color(0.36f, 0.24f, 0.12f, 1f);
    private static final Color LEAVES_COLOR = new Color(0.20f, 0.48f, 0.18f, 1f);
    private static final Color LAMP_COLOR = new Color(1.0f, 0.78f, 0.38f, 1f);

    private final int width;
    private final int depth;
    private final byte[] heights;
    private final Map<Long, Byte> edits = new HashMap<>();
    private int topY;

    public World(int width, int depth) {
        this.width = width;
        this.depth = depth;
        this.heights = new byte[width * depth];
        generate();
        generateTrees();
    }

    public int width() {
        return width;
    }

    public int depth() {
        return depth;
    }

    public int heightAt(int x, int z) {
        if (x < 0 || z < 0 || x >= width || z >= depth) {
            return 0;
        }
        return heights[x + z * width] & 0xFF;
    }

    public byte blockAt(int x, int y, int z) {
        if (x < 0 || z < 0 || x >= width || z >= depth || y < 0 || y >= MAX_Y) {
            return AIR;
        }
        Byte edit = edits.get(key(x, y, z));
        if (edit != null) {
            return edit;
        }
        return baseBlock(x, y, z);
    }

    public boolean setBlock(int x, int y, int z, byte type) {
        if (x < 0 || z < 0 || x >= width || z >= depth || y < 0 || y >= MAX_Y) {
            return false;
        }
        long key = key(x, y, z);
        if (type == baseBlock(x, y, z)) {
            edits.remove(key);
        } else {
            edits.put(key, type);
        }
        return true;
    }

    public Hit raycast(float ox, float oy, float oz, float dx, float dy, float dz, float maxDistance) {
        float step = 0.05f;
        int lastX = MathUtils.floor(ox);
        int lastY = MathUtils.floor(oy);
        int lastZ = MathUtils.floor(oz);

        for (float travel = step; travel <= maxDistance; travel += step) {
            float x = ox + dx * travel;
            float y = oy + dy * travel;
            float z = oz + dz * travel;
            int blockX = MathUtils.floor(x);
            int blockY = MathUtils.floor(y);
            int blockZ = MathUtils.floor(z);
            if (blockX == lastX && blockY == lastY && blockZ == lastZ) {
                continue;
            }
            if (blockAt(blockX, blockY, blockZ) != AIR) {
                return new Hit(blockX, blockY, blockZ, lastX, lastY, lastZ);
            }
            lastX = blockX;
            lastY = blockY;
            lastZ = blockZ;
        }
        return null;
    }

    public Mesh buildMesh() {
        FloatArray vertices = new FloatArray();
        ShortArray indices = new ShortArray();

        for (int x = 0; x < width; x++) {
            for (int z = 0; z < depth; z++) {
                for (int y = 0; y <= topY; y++) {
                    byte type = blockAt(x, y, z);
                    if (type == AIR) {
                        continue;
                    }
                    float variation = 0.88f + 0.12f * hash(x, y, z);
                    if (!meshSolid(x, y + 1, z)) {
                        face(vertices, indices, x, y, z, TOP, type, variation);
                    }
                    if (!meshSolid(x, y - 1, z)) {
                        face(vertices, indices, x, y, z, BOTTOM, type, variation);
                    }
                    if (!meshSolid(x, y, z - 1)) {
                        face(vertices, indices, x, y, z, NORTH, type, variation);
                    }
                    if (!meshSolid(x, y, z + 1)) {
                        face(vertices, indices, x, y, z, SOUTH, type, variation);
                    }
                    if (!meshSolid(x - 1, y, z)) {
                        face(vertices, indices, x, y, z, WEST, type, variation);
                    }
                    if (!meshSolid(x + 1, y, z)) {
                        face(vertices, indices, x, y, z, EAST, type, variation);
                    }
                }
            }
        }

        int vertexCount = vertices.size / FLOATS_PER_VERTEX;
        Mesh mesh = new Mesh(true, vertexCount, indices.size,
                new VertexAttribute(Usage.Position, 3, "a_position"),
                new VertexAttribute(Usage.ColorUnpacked, 4, "a_color"),
                new VertexAttribute(Usage.Normal, 3, "a_normal"),
                new VertexAttribute(Usage.Generic, 1, "a_emissive"));
        mesh.setVertices(vertices.toArray());
        mesh.setIndices(indices.toArray());
        return mesh;
    }

    public List<Light> lights(float x, float y, float z, int max, float radius) {
        List<Light> result = new ArrayList<>();
        float radiusSquared = radius * radius;
        for (Map.Entry<Long, Byte> entry : edits.entrySet()) {
            if (entry.getValue() != LAMP) {
                continue;
            }
            long key = entry.getKey();
            int lampX = (int) (key & 0xFFFFL);
            int lampZ = (int) ((key >> 16) & 0xFFFFL);
            int lampY = (int) ((key >> 32) & 0xFFFFL);
            float dx = lampX + 0.5f - x;
            float dy = lampY + 0.5f - y;
            float dz = lampZ + 0.5f - z;
            float distanceSquared = dx * dx + dy * dy + dz * dz;
            if (distanceSquared > radiusSquared) {
                continue;
            }
            result.add(new Light(lampX + 0.5f, lampY + 0.5f, lampZ + 0.5f, distanceSquared));
        }
        result.sort(Comparator.comparingDouble(light -> light.distanceSquared));
        if (result.size() > max) {
            return new ArrayList<>(result.subList(0, max));
        }
        return result;
    }

    public static String blockName(byte type) {
        switch (type) {
            case GRASS:
                return "grass";
            case DIRT:
                return "dirt";
            case STONE:
                return "stone";
            case WOOD:
                return "wood";
            case LEAVES:
                return "leaves";
            case LAMP:
                return "lamp";
            default:
                return "air";
        }
    }

    private void generate() {
        int highest = 0;
        for (int x = 0; x < width; x++) {
            for (int z = 0; z < depth; z++) {
                double n = Math.sin(x * 0.18) * 2.2
                        + Math.cos(z * 0.15) * 2.0
                        + Math.sin((x + z) * 0.08) * 3.0;
                int height = (int) Math.round(8 + n);
                height = Math.max(2, Math.min(20, height));
                heights[x + z * width] = (byte) height;
                highest = Math.max(highest, height);
            }
        }
        topY = Math.min(MAX_Y - 1, highest + 8);
    }

    private void generateTrees() {
        Random random = new Random(1201);
        for (int x = 4; x < width - 4; x += 9) {
            for (int z = 4; z < depth - 4; z += 9) {
                if (random.nextInt(100) > 55) {
                    continue;
                }
                int surface = heightAt(x, z);
                if (surface < 5) {
                    continue;
                }
                tree(random, x, surface + 1, z);
            }
        }
    }

    private void tree(Random random, int x, int y, int z) {
        int trunk = 4 + random.nextInt(2);
        for (int i = 0; i < trunk; i++) {
            setBlock(x, y + i, z, WOOD);
        }

        int top = y + trunk - 1;
        for (int dy = -1; dy <= 2; dy++) {
            int radius = dy <= 0 ? 2 : 1;
            for (int dx = -radius; dx <= radius; dx++) {
                for (int dz = -radius; dz <= radius; dz++) {
                    if (dx == 0 && dz == 0 && dy < 2) {
                        continue;
                    }
                    if (Math.abs(dx) == radius && Math.abs(dz) == radius && dy <= 0) {
                        continue;
                    }
                    if (blockAt(x + dx, top + dy, z + dz) == AIR) {
                        setBlock(x + dx, top + dy, z + dz, LEAVES);
                    }
                }
            }
        }
    }

    private byte baseBlock(int x, int y, int z) {
        int height = heightAt(x, z);
        if (y > height) {
            return AIR;
        }
        if (y == height) {
            return GRASS;
        }
        if (y >= height - 2) {
            return DIRT;
        }
        return STONE;
    }

    private boolean meshSolid(int x, int y, int z) {
        if (x < 0 || z < 0 || x >= width || z >= depth || y < 0) {
            return true;
        }
        if (y >= MAX_Y) {
            return false;
        }
        return blockAt(x, y, z) != AIR;
    }

    private static Color colorFor(byte type, int face) {
        switch (type) {
            case GRASS:
                return face == TOP ? GRASS_TOP : DIRT_COLOR;
            case DIRT:
                return DIRT_COLOR;
            case WOOD:
                return WOOD_COLOR;
            case LEAVES:
                return LEAVES_COLOR;
            case LAMP:
                return LAMP_COLOR;
            default:
                return STONE_COLOR;
        }
    }

    private static void face(FloatArray vertices, ShortArray indices, int x, int y, int z,
                             int face, byte type, float variation) {
        Color color = colorFor(type, face);
        float red = color.r * variation;
        float green = color.g * variation;
        float blue = color.b * variation;
        float emissive = type == LAMP ? 1f : 0f;

        int base = vertices.size / FLOATS_PER_VERTEX;
        switch (face) {
            case TOP:
                vertex(vertices, x, y + 1, z, red, green, blue, 0f, 1f, 0f, emissive);
                vertex(vertices, x + 1, y + 1, z, red, green, blue, 0f, 1f, 0f, emissive);
                vertex(vertices, x + 1, y + 1, z + 1, red, green, blue, 0f, 1f, 0f, emissive);
                vertex(vertices, x, y + 1, z + 1, red, green, blue, 0f, 1f, 0f, emissive);
                break;
            case BOTTOM:
                vertex(vertices, x, y, z, red, green, blue, 0f, -1f, 0f, emissive);
                vertex(vertices, x, y, z + 1, red, green, blue, 0f, -1f, 0f, emissive);
                vertex(vertices, x + 1, y, z + 1, red, green, blue, 0f, -1f, 0f, emissive);
                vertex(vertices, x + 1, y, z, red, green, blue, 0f, -1f, 0f, emissive);
                break;
            case NORTH:
                vertex(vertices, x, y, z, red, green, blue, 0f, 0f, -1f, emissive);
                vertex(vertices, x + 1, y, z, red, green, blue, 0f, 0f, -1f, emissive);
                vertex(vertices, x + 1, y + 1, z, red, green, blue, 0f, 0f, -1f, emissive);
                vertex(vertices, x, y + 1, z, red, green, blue, 0f, 0f, -1f, emissive);
                break;
            case SOUTH:
                vertex(vertices, x, y, z + 1, red, green, blue, 0f, 0f, 1f, emissive);
                vertex(vertices, x, y + 1, z + 1, red, green, blue, 0f, 0f, 1f, emissive);
                vertex(vertices, x + 1, y + 1, z + 1, red, green, blue, 0f, 0f, 1f, emissive);
                vertex(vertices, x + 1, y, z + 1, red, green, blue, 0f, 0f, 1f, emissive);
                break;
            case WEST:
                vertex(vertices, x, y, z, red, green, blue, -1f, 0f, 0f, emissive);
                vertex(vertices, x, y + 1, z, red, green, blue, -1f, 0f, 0f, emissive);
                vertex(vertices, x, y + 1, z + 1, red, green, blue, -1f, 0f, 0f, emissive);
                vertex(vertices, x, y, z + 1, red, green, blue, -1f, 0f, 0f, emissive);
                break;
            default:
                vertex(vertices, x + 1, y, z, red, green, blue, 1f, 0f, 0f, emissive);
                vertex(vertices, x + 1, y, z + 1, red, green, blue, 1f, 0f, 0f, emissive);
                vertex(vertices, x + 1, y + 1, z + 1, red, green, blue, 1f, 0f, 0f, emissive);
                vertex(vertices, x + 1, y + 1, z, red, green, blue, 1f, 0f, 0f, emissive);
                break;
        }

        indices.add((short) base);
        indices.add((short) (base + 1));
        indices.add((short) (base + 2));
        indices.add((short) base);
        indices.add((short) (base + 2));
        indices.add((short) (base + 3));
    }

    private static void vertex(FloatArray vertices, float x, float y, float z,
                               float red, float green, float blue,
                               float normalX, float normalY, float normalZ, float emissive) {
        vertices.add(x);
        vertices.add(y);
        vertices.add(z);
        vertices.add(red);
        vertices.add(green);
        vertices.add(blue);
        vertices.add(1f);
        vertices.add(normalX);
        vertices.add(normalY);
        vertices.add(normalZ);
        vertices.add(emissive);
    }

    private static long key(int x, int y, int z) {
        return ((long) x & 0xFFFFL) | (((long) z & 0xFFFFL) << 16) | (((long) y & 0xFFFFL) << 32);
    }

    private static float hash(int x, int y, int z) {
        int value = x * 374761393 + y * 668265263 + z * 2147483647;
        value = (value ^ (value >>> 13)) * 1274126177;
        return ((value ^ (value >>> 16)) & 0xFFFF) / 65535f;
    }

    /** One point light at a lamp block. */
    public static final class Light {
        public static final float RADIUS = 12f;

        public final float x;
        public final float y;
        public final float z;
        private final float distanceSquared;

        Light(float x, float y, float z, float distanceSquared) {
            this.x = x;
            this.y = y;
            this.z = z;
            this.distanceSquared = distanceSquared;
        }
    }

    /** One block hit by a ray, plus the air cell in front of it. */
    public static final class Hit {
        public final int x;
        public final int y;
        public final int z;
        public final int placeX;
        public final int placeY;
        public final int placeZ;

        Hit(int x, int y, int z, int placeX, int placeY, int placeZ) {
            this.x = x;
            this.y = y;
            this.z = z;
            this.placeX = placeX;
            this.placeY = placeY;
            this.placeZ = placeZ;
        }
    }
}
