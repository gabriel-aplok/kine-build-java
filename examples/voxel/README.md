# Kine Voxel

A small first-person voxel world built with libGDX and Kine. It has editable blocks, procedural trees, sun and lamp lighting, a target outline, a crosshair, and a HUD.

The project is a three-module workspace:

- `core`: the platform independent game code.
- `desktop`: the LWJGL3 launcher.
- `android`: the Android launcher, manifest, and touch controls.

## Run

Install Kine once from the repository root and put the binary on your `PATH`:

```sh
go build -o kine ./cmd/kine
```

On Windows, build `kine.exe` and add its directory to `PATH`.

Then from this directory:

```sh
kine run --project desktop
```

This builds the workspace and runs the desktop launcher. Without `--project`, `run` selects every `run` task, including the Android install. The first build resolves libGDX from Maven Central.

## Controls

- `W`, `A`, `S`, `D`: move
- Mouse: look
- Left mouse: break the targeted block
- Right mouse: place the selected block
- `1`, `2`, `3`, `4`: select grass, dirt, stone, wood
- `5`: select the lamp block
- `Space`: jump, or move up when flying
- `Shift`: move down when flying
- `F`: toggle flying
- `N`: toggle day and night
- `Esc`: release or capture the cursor

## Android

Build and launch on a connected device from this directory:

```sh
kine run --project android
```

The task builds the APK, installs it with `adb`, and starts the launcher activity. It picks the only ready device. To choose one of several devices:

```sh
kine run --project android --device SERIAL
```

To build the APK only:

```sh
kine
adb install -r android/build/outputs/apk/debug/app-debug.apk
```

The APK packages the arm64-v8a libGDX native library. On the phone, use the floating joystick on the left, drag on the right to look, and use the JUMP, BREAK, PLACE, and BLOCK buttons.

## Lighting

The sun moves across the sky over an eight minute day. Light direction, sun color, ambient fill, and sky color follow the time of day, including sunrise and sunset. Press `N` to jump between noon and midnight. Lamps glow on their own and cast light on nearby blocks. Select the lamp with `5` and place it with right-click.

## Screenshot

```sh
kine run -- --screenshot shot.png
```

The game renders ten frames, writes the image, and exits.

For a sunset shot:

```sh
kine run -- --screenshot sunset.png --time 0.49
```

For a night shot with four demo lamps around the spawn:

```sh
kine run -- --screenshot night.png --night --lights
```

On a machine without a usable X display, use Xvfb and software rendering:

```sh
xvfb-run -a -s "-screen 0 1280x720x24" env LIBGL_ALWAYS_SOFTWARE=1 kine run
```

## Physics check

```sh
kine run -- --physics-test
```

The check walks into a tree trunk and confirms the player stops at the trunk and does not climb it.

## Build only

```sh
kine
```

The outputs are:

- `core/build/libs/voxel-core.jar`
- `desktop/build/libs/voxel-desktop.jar`
- `android/build/outputs/apk/debug/app-debug.apk`
