package dev.kine.plugin;

import java.nio.file.Path;

/** Runtime context for a task action. */
public interface Context {
    Path dir();

    void log(String message);
}
