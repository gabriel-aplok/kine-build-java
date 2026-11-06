# Examples

Each directory is a standalone workspace. Build it from that directory with `kine`.

| Directory | What it shows |
|---|---|
| [demo](demo) | Two JVM projects with a project dependency |
| [desktop-demo](desktop-demo) | Desktop app image through `jpackage` |
| [android-demo](android-demo) | Android APK |
| [plugin-demo](plugin-demo) | JVM plugin with a custom task |
| [minecraft-demo](minecraft-demo) | Fabric mod built against a local game jar |
| [compat-maven](compat-maven) | Maven import |
| [compat-gradle](compat-gradle) | Gradle import |
| [voxel](voxel) | libGDX voxel game for desktop and Android |

Build Kine from the repository root and put the binary on your `PATH`:

```sh
go build -o kine ./cmd/kine
```

On Windows, build `kine.exe` instead. Then, for example:

```sh
cd examples/demo
kine run
```

The Android and Minecraft examples need a JDK. The Android example also needs an Android SDK and a connected device for `kine run`.
