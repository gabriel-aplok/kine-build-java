package dev.kine.plugin;

import java.util.ArrayList;
import java.util.Arrays;
import java.util.List;

/** One unit of work defined by a plugin. */
public final class Task {
    private final String id;
    private final List<String> deps = new ArrayList<>();
    private final List<PathSpec> inputs = new ArrayList<>();
    private final List<PathSpec> outputs = new ArrayList<>();
    private String key = "";
    private Action action;

    private Task(String id) {
        this.id = id;
    }

    public static Task create(String id) {
        return new Task(id);
    }

    public Task deps(String... values) {
        deps.addAll(Arrays.asList(values));
        return this;
    }

    public Task input(String path) {
        inputs.add(new PathSpec(path, false));
        return this;
    }

    public Task inputTree(String path) {
        inputs.add(new PathSpec(path, true));
        return this;
    }

    public Task output(String path) {
        outputs.add(new PathSpec(path, false));
        return this;
    }

    public Task outputTree(String path) {
        outputs.add(new PathSpec(path, true));
        return this;
    }

    public Task key(String value) {
        this.key = value;
        return this;
    }

    public Task action(Action value) {
        this.action = value;
        return this;
    }

    public String id() {
        return id;
    }

    public List<String> deps() {
        return deps;
    }

    public List<PathSpec> inputs() {
        return inputs;
    }

    public List<PathSpec> outputs() {
        return outputs;
    }

    public String key() {
        return key;
    }

    public Action action() {
        return action;
    }

    /** One relative path and whether it is a tree. */
    public static final class PathSpec {
        private final String path;
        private final boolean tree;

        PathSpec(String path, boolean tree) {
            this.path = path;
            this.tree = tree;
        }

        public String path() {
            return path;
        }

        public boolean tree() {
            return tree;
        }
    }
}
