package dev.kine.plugin;

/** Collects the providers declared by a plugin. */
public interface Registry {
    void provider(String id, Provider provider);
}
