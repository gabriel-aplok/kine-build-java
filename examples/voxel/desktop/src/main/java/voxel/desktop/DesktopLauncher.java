package voxel.desktop;

import com.badlogic.gdx.backends.lwjgl3.Lwjgl3Application;
import com.badlogic.gdx.backends.lwjgl3.Lwjgl3ApplicationConfiguration;
import voxel.VoxelGame;

public final class DesktopLauncher {
    private DesktopLauncher() {
    }

    public static void main(String[] args) {
        VoxelGame.Config config = new VoxelGame.Config();
        boolean physicsTest = false;

        for (int i = 0; i < args.length; i++) {
            switch (args[i]) {
                case "--screenshot":
                    if (i + 1 < args.length) {
                        config.screenshotPath = args[i + 1];
                    }
                    break;
                case "--night":
                    config.startTime = 0.75f;
                    break;
                case "--time":
                    if (i + 1 < args.length) {
                        try {
                            config.startTime = Float.parseFloat(args[i + 1]);
                        } catch (NumberFormatException error) {
                            System.err.println("invalid --time: " + args[i + 1]);
                        }
                    }
                    break;
                case "--lights":
                    config.demoLights = true;
                    break;
                case "--physics-test":
                    physicsTest = true;
                    break;
                default:
                    break;
            }
        }

        if (physicsTest) {
            System.exit(VoxelGame.runPhysicsTest() ? 0 : 1);
        }

        Lwjgl3ApplicationConfiguration window = new Lwjgl3ApplicationConfiguration();
        window.setTitle("Kine Voxel");
        window.setWindowedMode(1280, 720);
        window.setForegroundFPS(60);
        window.useVsync(true);
        new Lwjgl3Application(new VoxelGame(config), window);
    }
}
