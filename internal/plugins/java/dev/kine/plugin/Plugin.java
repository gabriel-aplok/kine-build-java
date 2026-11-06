package dev.kine.plugin;

/** Entry point implemented by every plugin. */
public interface Plugin {
    void register(Registry registry);
}
