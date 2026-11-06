package dev.kine.plugin;

import java.util.Map;

/** One project passed to a provider. */
public final class Project {
    private final String id;
    private final String dir;
    private final Map<String, Object> options;

    public Project(String id, String dir, Map<String, Object> options) {
        this.id = id;
        this.dir = dir;
        this.options = options;
    }

    public String id() {
        return id;
    }

    public String dir() {
        return dir;
    }

    public Map<String, Object> options() {
        return options;
    }
}
