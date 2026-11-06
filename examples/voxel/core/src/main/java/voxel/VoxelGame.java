package voxel;

import com.badlogic.gdx.Application;
import com.badlogic.gdx.ApplicationAdapter;
import com.badlogic.gdx.Gdx;
import com.badlogic.gdx.Input;
import com.badlogic.gdx.InputAdapter;
import com.badlogic.gdx.graphics.Color;
import com.badlogic.gdx.graphics.GL20;
import com.badlogic.gdx.graphics.Mesh;
import com.badlogic.gdx.graphics.PerspectiveCamera;
import com.badlogic.gdx.graphics.Pixmap;
import com.badlogic.gdx.graphics.PixmapIO;
import com.badlogic.gdx.graphics.g2d.BitmapFont;
import com.badlogic.gdx.graphics.g2d.SpriteBatch;
import com.badlogic.gdx.graphics.glutils.ShaderProgram;
import com.badlogic.gdx.graphics.glutils.ShapeRenderer;
import com.badlogic.gdx.math.MathUtils;
import com.badlogic.gdx.math.Matrix4;
import com.badlogic.gdx.math.Vector2;
import com.badlogic.gdx.math.Vector3;
import com.badlogic.gdx.utils.ScreenUtils;

import java.io.File;
import java.nio.ByteBuffer;
import java.util.List;

/** A first-person voxel world with editing, trees, and a small HUD. */
public final class VoxelGame extends ApplicationAdapter {
    private static final float EYE_HEIGHT = 1.62f;
    private static final float WALK_SPEED = 6f;
    private static final float FLY_SPEED = 12f;
    private static final float GRAVITY = 22f;
    private static final float JUMP_SPEED = 7f;
    private static final float LOOK_SENSITIVITY = 0.12f;
    private static final float REACH = 8f;
    private static final float PLAYER_RADIUS = 0.3f;
    private static final float PLAYER_HEIGHT = 1.8f;
    private static final float MOVE_STEP = 0.05f;
    private static final float EPSILON = 0.001f;
    private static final float TERMINAL_VELOCITY = -30f;

    private static final Color DAY_SKY = new Color(0.53f, 0.74f, 0.94f, 1f);
    private static final Color NIGHT_SKY = new Color(0.04f, 0.06f, 0.12f, 1f);
    private static final Color SUNSET_SKY = new Color(0.95f, 0.55f, 0.35f, 1f);
    private static final Color DAY_SUN = new Color(0.70f, 0.67f, 0.58f, 1f);
    private static final Color SUNSET_SUN = new Color(1.0f, 0.55f, 0.25f, 1f);
    private static final Color NIGHT_SUN = new Color(0.03f, 0.04f, 0.08f, 1f);
    private static final Color DAY_AMBIENT = new Color(0.34f, 0.38f, 0.44f, 1f);
    private static final Color NIGHT_AMBIENT = new Color(0.13f, 0.15f, 0.21f, 1f);
    private static final float DAY_LENGTH = 480f;
    private static final int MAX_LIGHTS = 8;
    private static final float JOYSTICK_RADIUS = 90f;
    private static final float TOUCH_LOOK = 0.16f;
    private static final float BUTTON_WIDTH = 84f;
    private static final float BUTTON_HEIGHT = 64f;

    public static final class Config {
        public String screenshotPath;
        public float startTime = 0.22f;
        public boolean demoLights;
    }

    private final Config config;
    private World world;
    private Mesh mesh;
    private PerspectiveCamera camera;
    private ShaderProgram shader;
    private ShapeRenderer shapes;
    private SpriteBatch batch;
    private BitmapFont font;
    private final Matrix4 uiMatrix = new Matrix4();

    private final Vector3 position = new Vector3();
    private float velocityY;
    private float yaw;
    private float pitch;
    private boolean flying;
    private boolean onGround;
    private byte selected = World.GRASS;
    private final float[] lightPositions = new float[MAX_LIGHTS * 3];
    private final float[] lightRadii = new float[MAX_LIGHTS];
    private final Vector3 sunDirection = new Vector3();
    private final Color skyColor = new Color();
    private final Color sunColor = new Color();
    private final Color ambient = new Color();
    private float timeOfDay = 0.22f;
    private int frame;

    private boolean android;
    private final Vector2 touchMove = new Vector2();
    private final Vector2 joystickOrigin = new Vector2();
    private final Vector2 joystickKnob = new Vector2();
    private int joystickPointer = -1;
    private int lookPointer = -1;
    private float lastLookX;
    private float lastLookY;
    private boolean touchJump;
    private boolean touchBreak;
    private boolean touchPlace;

    public VoxelGame() {
        this(new Config());
    }

    public VoxelGame(Config config) {
        this.config = config;
    }

    @Override
    public void create() {
        world = new World(64, 64);
        if (config.startTime >= 0f) {
            timeOfDay = ((config.startTime % 1f) + 1f) % 1f;
        }
        updateLighting();

        camera = new PerspectiveCamera(70f, Gdx.graphics.getWidth(), Gdx.graphics.getHeight());
        camera.near = 0.05f;
        camera.far = 400f;
        camera.up.set(Vector3.Y);

        int spawnX = world.width() / 2;
        int spawnZ = world.depth() / 2;
        int highest = -1;
        for (int x = 1; x < world.width() - 1; x++) {
            for (int z = 1; z < world.depth() - 1; z++) {
                int height = world.heightAt(x, z);
                if (height > highest && world.blockAt(x, height + 1, z) == World.AIR) {
                    highest = height;
                    spawnX = x;
                    spawnZ = z;
                }
            }
        }
        if (highest < 0) {
            highest = world.heightAt(spawnX, spawnZ);
        }
        position.set(spawnX + 0.5f, highest + 1f, spawnZ + 0.5f);
        yaw = 45f;
        pitch = -28f;
        updateCamera();

        if (config.demoLights) {
            int[][] offsets = {{-3, 0}, {3, 0}, {0, -3}, {0, 3}};
            for (int[] offset : offsets) {
                int lampX = spawnX + offset[0];
                int lampZ = spawnZ + offset[1];
                int lampY = world.heightAt(lampX, lampZ) + 1;
                world.setBlock(lampX, lampY, lampZ, World.LAMP);
            }
        }
        mesh = world.buildMesh();

        ShaderProgram.pedantic = false;
        shader = new ShaderProgram(
                "attribute vec3 a_position;\n"
                        + "attribute vec4 a_color;\n"
                        + "attribute vec3 a_normal;\n"
                        + "attribute float a_emissive;\n"
                        + "uniform mat4 u_projTrans;\n"
                        + "varying vec4 v_color;\n"
                        + "varying vec3 v_world;\n"
                        + "varying vec3 v_normal;\n"
                        + "varying float v_emissive;\n"
                        + "void main() {\n"
                        + "  v_color = a_color;\n"
                        + "  v_world = a_position;\n"
                        + "  v_normal = a_normal;\n"
                        + "  v_emissive = a_emissive;\n"
                        + "  gl_Position = u_projTrans * vec4(a_position, 1.0);\n"
                        + "}\n",
                "#ifdef GL_ES\n"
                        + "precision mediump float;\n"
                        + "#endif\n"
                        + "uniform vec3 u_camera;\n"
                        + "uniform vec3 u_fogColor;\n"
                        + "uniform vec3 u_sunDir;\n"
                        + "uniform vec3 u_sunColor;\n"
                        + "uniform vec3 u_ambient;\n"
                        + "uniform vec3 u_lightPos[8];\n"
                        + "uniform float u_lightRadius[8];\n"
                        + "uniform int u_lightCount;\n"
                        + "varying vec4 v_color;\n"
                        + "varying vec3 v_world;\n"
                        + "varying vec3 v_normal;\n"
                        + "varying float v_emissive;\n"
                        + "void main() {\n"
                        + "  vec3 normal = normalize(v_normal);\n"
                        + "  vec3 light = u_ambient + u_sunColor * max(dot(normal, u_sunDir), 0.0);\n"
                        + "  for (int i = 0; i < 8; i++) {\n"
                        + "    if (i >= u_lightCount) break;\n"
                        + "    vec3 delta = u_lightPos[i] - v_world;\n"
                        + "    float distance = length(delta);\n"
                        + "    float attenuation = clamp(1.0 - distance / u_lightRadius[i], 0.0, 1.0);\n"
                        + "    attenuation *= attenuation;\n"
                        + "    float diffuse = max(dot(normal, normalize(delta)), 0.0);\n"
                        + "    light += vec3(1.0, 0.55, 0.2) * attenuation * (0.25 + 0.75 * diffuse);\n"
                        + "  }\n"
                        + "  vec3 lit = v_color.rgb * light;\n"
                        + "  vec3 color = mix(lit, v_color.rgb * 1.5, v_emissive);\n"
                        + "  color = min(color, vec3(1.0));\n"
                        + "  float fog = clamp((length(v_world - u_camera) - 70.0) / 130.0, 0.0, 0.75);\n"
                        + "  gl_FragColor = vec4(mix(color, u_fogColor, fog), 1.0);\n"
                        + "}\n");
        if (!shader.isCompiled()) {
            throw new IllegalStateException("shader: " + shader.getLog());
        }

        shapes = new ShapeRenderer();
        batch = new SpriteBatch();
        font = new BitmapFont();
        font.setColor(Color.WHITE);
        updateUiMatrix();

        Gdx.gl.glEnable(GL20.GL_DEPTH_TEST);
        Gdx.gl.glDepthFunc(GL20.GL_LEQUAL);
        Gdx.gl.glDisable(GL20.GL_CULL_FACE);
        android = Gdx.app.getType() == Application.ApplicationType.Android;
        if (android) {
            Gdx.input.setInputProcessor(new TouchControls());
        } else if (Gdx.app.getType() == Application.ApplicationType.Desktop) {
            Gdx.input.setCursorCatched(true);
        }
    }

    @Override
    public void render() {
        float delta = Math.min(Gdx.graphics.getDeltaTime(), 0.05f);
        look();
        move(delta);
        updateCamera();
        timeOfDay = (timeOfDay + delta / DAY_LENGTH) % 1f;
        updateLighting();

        if (Gdx.input.isKeyJustPressed(Input.Keys.NUM_1)) {
            selected = World.GRASS;
        } else if (Gdx.input.isKeyJustPressed(Input.Keys.NUM_2)) {
            selected = World.DIRT;
        } else if (Gdx.input.isKeyJustPressed(Input.Keys.NUM_3)) {
            selected = World.STONE;
        } else if (Gdx.input.isKeyJustPressed(Input.Keys.NUM_4)) {
            selected = World.WOOD;
        } else if (Gdx.input.isKeyJustPressed(Input.Keys.NUM_5)) {
            selected = World.LAMP;
        }

        World.Hit target = world.raycast(
                camera.position.x, camera.position.y, camera.position.z,
                camera.direction.x, camera.direction.y, camera.direction.z,
                REACH);

        boolean breakPressed = Gdx.input.isButtonJustPressed(Input.Buttons.LEFT) || touchBreak;
        boolean placePressed = Gdx.input.isButtonJustPressed(Input.Buttons.RIGHT) || touchPlace;
        touchBreak = false;
        touchPlace = false;

        if (target != null) {
            if (breakPressed) {
                world.setBlock(target.x, target.y, target.z, World.AIR);
                rebuildMesh();
                target = world.raycast(
                        camera.position.x, camera.position.y, camera.position.z,
                        camera.direction.x, camera.direction.y, camera.direction.z,
                        REACH);
            } else if (placePressed) {
                if (!intersectsPlayer(target.placeX, target.placeY, target.placeZ)) {
                    world.setBlock(target.placeX, target.placeY, target.placeZ, selected);
                    rebuildMesh();
                    target = world.raycast(
                            camera.position.x, camera.position.y, camera.position.z,
                            camera.direction.x, camera.direction.y, camera.direction.z,
                            REACH);
                }
            }
        }

        Gdx.gl.glEnable(GL20.GL_DEPTH_TEST);
        Gdx.gl.glClearColor(skyColor.r, skyColor.g, skyColor.b, 1f);
        Gdx.gl.glClear(GL20.GL_COLOR_BUFFER_BIT | GL20.GL_DEPTH_BUFFER_BIT);

        List<World.Light> lights = world.lights(
                camera.position.x, camera.position.y, camera.position.z,
                MAX_LIGHTS, World.Light.RADIUS);
        int lightCount = lights.size();
        for (int i = 0; i < lightCount; i++) {
            World.Light light = lights.get(i);
            lightPositions[i * 3] = light.x;
            lightPositions[i * 3 + 1] = light.y;
            lightPositions[i * 3 + 2] = light.z;
            lightRadii[i] = World.Light.RADIUS;
        }

        shader.begin();
        shader.setUniformMatrix("u_projTrans", camera.combined);
        shader.setUniformf("u_camera", camera.position.x, camera.position.y, camera.position.z);
        shader.setUniformf("u_fogColor", skyColor.r, skyColor.g, skyColor.b);
        shader.setUniformf("u_sunDir", sunDirection.x, sunDirection.y, sunDirection.z);
        shader.setUniformf("u_sunColor", sunColor.r, sunColor.g, sunColor.b);
        shader.setUniformf("u_ambient", ambient.r, ambient.g, ambient.b);
        shader.setUniformi("u_lightCount", lightCount);
        if (lightCount > 0) {
            shader.setUniform3fv("u_lightPos", lightPositions, 0, lightCount);
            shader.setUniform1fv("u_lightRadius", lightRadii, 0, lightCount);
        }
        mesh.render(shader, GL20.GL_TRIANGLES);
        shader.end();

        drawTarget(target);
        drawCrosshair();
        if (android) {
            drawTouchControls();
        }
        drawHud();

        frame++;
        if (config.screenshotPath != null && frame == 10) {
            saveScreenshot(config.screenshotPath);
            Gdx.app.exit();
        }
    }

    @Override
    public void resize(int width, int height) {
        camera.viewportWidth = width;
        camera.viewportHeight = height;
        camera.update();
        updateUiMatrix();
    }

    @Override
    public void dispose() {
        if (mesh != null) {
            mesh.dispose();
        }
        if (shader != null) {
            shader.dispose();
        }
        if (shapes != null) {
            shapes.dispose();
        }
        if (batch != null) {
            batch.dispose();
        }
        if (font != null) {
            font.dispose();
        }
    }

    private void look() {
        if (Gdx.app.getType() != Application.ApplicationType.Desktop) {
            return;
        }
        if (Gdx.input.isKeyJustPressed(Input.Keys.ESCAPE)) {
            Gdx.input.setCursorCatched(!Gdx.input.isCursorCatched());
        }
        if (Gdx.input.isKeyJustPressed(Input.Keys.N)) {
            timeOfDay = MathUtils.sin(timeOfDay * MathUtils.PI2) > 0f ? 0.75f : 0.25f;
        }
        if (!Gdx.input.isCursorCatched()) {
            return;
        }
        yaw += Gdx.input.getDeltaX() * LOOK_SENSITIVITY;
        pitch -= Gdx.input.getDeltaY() * LOOK_SENSITIVITY;
        pitch = MathUtils.clamp(pitch, -89f, 89f);
    }

    private void move(float delta) {
        float forward = touchMove.y;
        float strafe = touchMove.x;
        if (Gdx.input.isKeyPressed(Input.Keys.W)) {
            forward += 1f;
        }
        if (Gdx.input.isKeyPressed(Input.Keys.S)) {
            forward -= 1f;
        }
        if (Gdx.input.isKeyPressed(Input.Keys.A)) {
            strafe -= 1f;
        }
        if (Gdx.input.isKeyPressed(Input.Keys.D)) {
            strafe += 1f;
        }
        boolean jump = Gdx.input.isKeyJustPressed(Input.Keys.SPACE) || touchJump;
        touchJump = false;
        applyMovement(delta, forward, strafe,
                jump,
                Gdx.input.isKeyPressed(Input.Keys.SPACE),
                Gdx.input.isKeyPressed(Input.Keys.SHIFT_LEFT),
                Gdx.input.isKeyJustPressed(Input.Keys.F));
    }

    private void applyMovement(float delta, float forward, float strafe,
                               boolean jump, boolean up, boolean down, boolean toggleFly) {
        if (toggleFly) {
            flying = !flying;
            velocityY = 0f;
        }

        float radians = yaw * MathUtils.degreesToRadians;
        float dx = MathUtils.cos(radians) * forward - MathUtils.sin(radians) * strafe;
        float dz = MathUtils.sin(radians) * forward + MathUtils.cos(radians) * strafe;
        float length = (float) Math.sqrt(dx * dx + dz * dz);
        if (length > 0f) {
            dx /= length;
            dz /= length;
        }

        float speed = flying ? FLY_SPEED : WALK_SPEED;
        position.x = moveAxis(position.x, dx * speed * delta, true);
        position.z = moveAxis(position.z, dz * speed * delta, false);

        if (flying) {
            float dy = 0f;
            if (up) {
                dy += FLY_SPEED * delta;
            }
            if (down) {
                dy -= FLY_SPEED * delta;
            }
            moveVertical(dy);
            velocityY = 0f;
        } else {
            if (onGround && jump) {
                velocityY = JUMP_SPEED;
                onGround = false;
            }
            velocityY = Math.max(TERMINAL_VELOCITY, velocityY - GRAVITY * delta);
            moveVertical(velocityY * delta);
        }

        position.x = MathUtils.clamp(position.x, 1.5f, world.width() - 1.5f);
        position.z = MathUtils.clamp(position.z, 1.5f, world.depth() - 1.5f);
    }

    private float moveAxis(float current, float delta, boolean xAxis) {
        float remaining = delta;
        while (Math.abs(remaining) > 0f) {
            float amount = MathUtils.clamp(remaining, -MOVE_STEP, MOVE_STEP);
            float candidate = current + amount;
            boolean free = xAxis
                    ? !collides(candidate, position.y, position.z)
                    : !collides(position.x, position.y, candidate);
            if (!free) {
                if (onGround) {
                    float stepY = MathUtils.floor(position.y) + 1f + EPSILON;
                    boolean stepFree = xAxis
                            ? !collides(candidate, stepY, position.z)
                            : !collides(position.x, stepY, candidate);
                    if (stepFree) {
                        position.y = stepY;
                        current = candidate;
                        remaining -= amount;
                        continue;
                    }
                }
                break;
            }
            current = candidate;
            remaining -= amount;
        }
        return current;
    }

    private void moveVertical(float delta) {
        if (delta == 0f) {
            return;
        }
        float candidate = position.y + delta;
        if (!collides(position.x, candidate, position.z)) {
            position.y = candidate;
            if (delta < 0f) {
                onGround = false;
            }
            return;
        }
        if (delta < 0f) {
            position.y = groundUnder(position.x, position.z, position.y);
            velocityY = 0f;
            onGround = true;
        } else {
            velocityY = 0f;
        }
    }

    private boolean collides(float x, float y, float z) {
        int x0 = MathUtils.floor(x - PLAYER_RADIUS);
        int x1 = MathUtils.floor(x + PLAYER_RADIUS - EPSILON);
        int y0 = MathUtils.floor(y);
        int y1 = MathUtils.floor(y + PLAYER_HEIGHT - EPSILON);
        int z0 = MathUtils.floor(z - PLAYER_RADIUS);
        int z1 = MathUtils.floor(z + PLAYER_RADIUS - EPSILON);
        for (int bx = x0; bx <= x1; bx++) {
            for (int by = y0; by <= y1; by++) {
                for (int bz = z0; bz <= z1; bz++) {
                    if (world.blockAt(bx, by, bz) != World.AIR) {
                        return true;
                    }
                }
            }
        }
        return false;
    }

    private float groundUnder(float x, float z, float fromY) {
        int x0 = MathUtils.floor(x - PLAYER_RADIUS);
        int x1 = MathUtils.floor(x + PLAYER_RADIUS - EPSILON);
        int z0 = MathUtils.floor(z - PLAYER_RADIUS);
        int z1 = MathUtils.floor(z + PLAYER_RADIUS - EPSILON);
        float top = 0f;
        for (int bx = x0; bx <= x1; bx++) {
            for (int bz = z0; bz <= z1; bz++) {
                for (int by = MathUtils.floor(fromY); by >= 0; by--) {
                    if (world.blockAt(bx, by, bz) != World.AIR) {
                        top = Math.max(top, by + 1f);
                        break;
                    }
                }
            }
        }
        return top;
    }

    private void updateCamera() {
        float radians = yaw * MathUtils.degreesToRadians;
        float pitchRadians = pitch * MathUtils.degreesToRadians;
        camera.position.set(position.x, position.y + EYE_HEIGHT, position.z);
        camera.direction.set(
                MathUtils.cos(radians) * MathUtils.cos(pitchRadians),
                MathUtils.sin(pitchRadians),
                MathUtils.sin(radians) * MathUtils.cos(pitchRadians)).nor();
        camera.update();
    }

    private void rebuildMesh() {
        mesh.dispose();
        mesh = world.buildMesh();
    }

    private boolean intersectsPlayer(int x, int y, int z) {
        int playerX = MathUtils.floor(position.x);
        int playerZ = MathUtils.floor(position.z);
        int feet = MathUtils.floor(position.y);
        return x == playerX && z == playerZ && (y == feet || y == feet + 1);
    }

    private void drawTarget(World.Hit target) {
        if (target == null) {
            return;
        }
        shapes.setProjectionMatrix(camera.combined);
        shapes.begin(ShapeRenderer.ShapeType.Line);
        shapes.setColor(0f, 0f, 0f, 0.55f);
        shapes.box(target.x - 0.004f, target.y - 0.004f, target.z - 0.004f,
                1.008f, 1.008f, 1.008f);
        shapes.end();
    }

    private void drawCrosshair() {
        Gdx.gl.glDisable(GL20.GL_DEPTH_TEST);
        shapes.setProjectionMatrix(uiMatrix);
        shapes.begin(ShapeRenderer.ShapeType.Line);
        shapes.setColor(1f, 1f, 1f, 0.85f);
        float centerX = Gdx.graphics.getWidth() / 2f;
        float centerY = Gdx.graphics.getHeight() / 2f;
        shapes.line(centerX - 8f, centerY, centerX + 8f, centerY);
        shapes.line(centerX, centerY - 8f, centerX, centerY + 8f);
        shapes.end();
    }

    private void drawHud() {
        int totalMinutes = (int) (timeOfDay * 24f * 60f + 6f * 60f) % (24 * 60);
        String clock = String.format("%02d:%02d", totalMinutes / 60, totalMinutes % 60);
        batch.setProjectionMatrix(uiMatrix);
        batch.begin();
        font.setColor(Color.WHITE);
        String blockLine = "Block: " + World.blockName(selected)
                + (android ? "" : "   [1-5]") + "   " + clock;
        String controls = android
                ? "Joystick move   Drag look   Buttons: jump break place block"
                : "WASD move   LMB break   RMB place   Space jump   F fly   N day/night   Esc cursor";
        font.draw(batch, blockLine, 12f, Gdx.graphics.getHeight() - 12f);
        font.draw(batch, controls + "   FPS " + Gdx.graphics.getFramesPerSecond(),
                12f, Gdx.graphics.getHeight() - 34f);
        batch.end();
    }

    private void drawTouchControls() {
        float baseX = joystickPointer >= 0 ? joystickOrigin.x : 140f;
        float baseY = joystickPointer >= 0 ? joystickOrigin.y : 140f;

        shapes.setProjectionMatrix(uiMatrix);
        shapes.begin(ShapeRenderer.ShapeType.Filled);
        shapes.setColor(0f, 0f, 0f, 0.22f);
        shapes.circle(baseX, baseY, JOYSTICK_RADIUS, 32);
        shapes.setColor(1f, 1f, 1f, 0.14f);
        shapes.circle(baseX, baseY, JOYSTICK_RADIUS * 0.55f, 24);
        if (joystickPointer >= 0) {
            shapes.setColor(1f, 1f, 1f, 0.35f);
            shapes.circle(joystickKnob.x, joystickKnob.y, 26f, 20);
        }
        for (int row = 0; row < 2; row++) {
            for (int column = 0; column < 2; column++) {
                shapes.setColor(0f, 0f, 0f, 0.35f);
                shapes.rect(buttonX(column), buttonY(row), BUTTON_WIDTH, BUTTON_HEIGHT);
            }
        }
        shapes.end();

        batch.setProjectionMatrix(uiMatrix);
        batch.begin();
        font.setColor(1f, 1f, 1f, 0.95f);
        font.draw(batch, "JUMP", buttonX(0) + 16f, buttonY(1) + 40f);
        font.draw(batch, "BLOCK", buttonX(1) + 12f, buttonY(1) + 40f);
        font.draw(batch, "BREAK", buttonX(1) + 12f, buttonY(0) + 40f);
        font.draw(batch, "PLACE", buttonX(0) + 12f, buttonY(0) + 40f);
        batch.end();
    }

    private float buttonX(int column) {
        return Gdx.graphics.getWidth() - 24f - (column + 1) * BUTTON_WIDTH - column * 12f;
    }

    private float buttonY(int row) {
        return 24f + row * (BUTTON_HEIGHT + 12f);
    }

    private boolean buttonHit(float x, float y, int column, int row) {
        return x >= buttonX(column) && x <= buttonX(column) + BUTTON_WIDTH
                && y >= buttonY(row) && y <= buttonY(row) + BUTTON_HEIGHT;
    }

    private static byte nextBlock(byte type) {
        switch (type) {
            case World.GRASS:
                return World.DIRT;
            case World.DIRT:
                return World.STONE;
            case World.STONE:
                return World.WOOD;
            case World.WOOD:
                return World.LAMP;
            default:
                return World.GRASS;
        }
    }

    private final class TouchControls extends InputAdapter {
        @Override
        public boolean touchDown(int screenX, int screenY, int pointer, int button) {
            float x = screenX;
            float y = Gdx.graphics.getHeight() - screenY;
            if (buttonHit(x, y, 0, 1)) {
                touchJump = true;
                return true;
            }
            if (buttonHit(x, y, 1, 1)) {
                selected = nextBlock(selected);
                return true;
            }
            if (buttonHit(x, y, 1, 0)) {
                touchBreak = true;
                return true;
            }
            if (buttonHit(x, y, 0, 0)) {
                touchPlace = true;
                return true;
            }
            if (x < Gdx.graphics.getWidth() / 2f) {
                if (joystickPointer < 0) {
                    joystickPointer = pointer;
                    joystickOrigin.set(x, y);
                    joystickKnob.set(x, y);
                    touchMove.setZero();
                }
            } else if (lookPointer < 0) {
                lookPointer = pointer;
                lastLookX = screenX;
                lastLookY = screenY;
            }
            return true;
        }

        @Override
        public boolean touchDragged(int screenX, int screenY, int pointer) {
            if (pointer == joystickPointer) {
                float y = Gdx.graphics.getHeight() - screenY;
                float dx = screenX - joystickOrigin.x;
                float dy = y - joystickOrigin.y;
                float length = (float) Math.sqrt(dx * dx + dy * dy);
                if (length > JOYSTICK_RADIUS) {
                    dx = dx / length * JOYSTICK_RADIUS;
                    dy = dy / length * JOYSTICK_RADIUS;
                }
                joystickKnob.set(joystickOrigin.x + dx, joystickOrigin.y + dy);
                touchMove.set(dx / JOYSTICK_RADIUS, dy / JOYSTICK_RADIUS);
            } else if (pointer == lookPointer) {
                yaw += (screenX - lastLookX) * TOUCH_LOOK;
                pitch -= (screenY - lastLookY) * TOUCH_LOOK;
                pitch = MathUtils.clamp(pitch, -89f, 89f);
                lastLookX = screenX;
                lastLookY = screenY;
            }
            return true;
        }

        @Override
        public boolean touchUp(int screenX, int screenY, int pointer, int button) {
            if (pointer == joystickPointer) {
                joystickPointer = -1;
                touchMove.setZero();
            }
            if (pointer == lookPointer) {
                lookPointer = -1;
            }
            return true;
        }
    }

    private void updateLighting() {
        float angle = timeOfDay * MathUtils.PI2;
        float elevation = MathUtils.sin(angle);
        sunDirection.set(MathUtils.cos(angle) * 0.6f, elevation, 0.35f).nor();

        float day = MathUtils.clamp(sunDirection.y, 0f, 1f);
        float smooth = day * day * (3f - 2f * day);
        float horizon = elevation > 0f
                ? 1f - MathUtils.clamp(elevation / 0.4f, 0f, 1f)
                : 0f;

        skyColor.set(NIGHT_SKY).lerp(DAY_SKY, smooth).lerp(SUNSET_SKY, horizon * 0.75f);
        ambient.set(NIGHT_AMBIENT).lerp(DAY_AMBIENT, smooth);
        float twilight = horizon * (1f - smooth);
        ambient.r += twilight * 0.20f;
        ambient.g += twilight * 0.11f;
        ambient.b += twilight * 0.05f;
        sunColor.set(DAY_SUN).lerp(SUNSET_SUN, horizon * 0.85f);
        float intensity = MathUtils.clamp(elevation * 2.5f, 0f, 1f);
        sunColor.r *= intensity;
        sunColor.g *= intensity;
        sunColor.b *= intensity;
    }

    private void updateUiMatrix() {
        uiMatrix.setToOrtho2D(0, 0, Gdx.graphics.getWidth(), Gdx.graphics.getHeight());
    }

    public static boolean runPhysicsTest() {
        World world = new World(64, 64);
        int startX = -1;
        int startZ = -1;
        int surface = 0;

        for (int x = 1; x < world.width() - 4 && startX < 0; x++) {
            for (int z = 1; z < world.depth() - 1; z++) {
                int height = world.heightAt(x, z);
                if (world.heightAt(x + 1, z) != height || world.heightAt(x + 2, z) != height) {
                    continue;
                }
                if (world.blockAt(x, height + 1, z) != World.AIR
                        || world.blockAt(x + 1, height + 1, z) != World.AIR
                        || world.blockAt(x + 2, height + 1, z) != World.AIR) {
                    continue;
                }
                startX = x;
                startZ = z;
                surface = height;
                break;
            }
        }
        if (startX < 0) {
            System.out.println("physics: no flat area found");
            return false;
        }

        VoxelGame game = new VoxelGame();
        game.world = world;
        game.position.set(startX + 0.5f, surface + 1f, startZ + 0.5f);
        game.yaw = 0f;
        game.onGround = true;

        int treeX = startX + 2;
        for (int y = surface + 1; y <= surface + 5; y++) {
            world.setBlock(treeX, y, startZ, World.WOOD);
        }
        world.setBlock(treeX, surface + 6, startZ, World.LEAVES);

        for (int i = 0; i < 180; i++) {
            game.applyMovement(1f / 60f, 1f, 0f, false, false, false, false);
        }

        boolean stopped = game.position.x < treeX;
        boolean stayedLow = game.position.y <= surface + 1.01f;
        System.out.println("physics: x=" + game.position.x + " y=" + game.position.y
                + " stopped=" + stopped + " stayedLow=" + stayedLow);
        return stopped && stayedLow;
    }

    private static void saveScreenshot(String path) {
        int width = Gdx.graphics.getBackBufferWidth();
        int height = Gdx.graphics.getBackBufferHeight();
        Pixmap source = ScreenUtils.getFrameBufferPixmap(0, 0, width, height);
        Pixmap flipped = new Pixmap(width, height, Pixmap.Format.RGBA8888);
        ByteBuffer from = source.getPixels();
        ByteBuffer to = flipped.getPixels();
        int stride = width * 4;
        for (int y = 0; y < height; y++) {
            int sourceRow = (height - 1 - y) * stride;
            int targetRow = y * stride;
            for (int i = 0; i < stride; i++) {
                to.put(targetRow + i, from.get(sourceRow + i));
            }
        }
        PixmapIO.writePNG(Gdx.files.absolute(new File(path).getAbsolutePath()), flipped);
        source.dispose();
        flipped.dispose();
    }
}
