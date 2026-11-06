package dev.kine.plugin.host;

import dev.kine.plugin.Context;
import dev.kine.plugin.Plugin;
import dev.kine.plugin.Provider;
import dev.kine.plugin.Registry;
import dev.kine.plugin.Task;

import java.io.BufferedReader;
import java.io.IOException;
import java.io.InputStream;
import java.io.InputStreamReader;
import java.io.PrintStream;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.locks.ReentrantLock;
import java.util.zip.ZipEntry;
import java.util.zip.ZipFile;

/** Loads plugins and serves the JSON-RPC protocol. */
final class Host {
    private static final int API_VERSION = 1;
    private static final int MAX_MANIFEST_SIZE = 64 * 1024;
    private static final String MANIFEST_PATH = "META-INF/kine-plugin.json";

    private final PrintStream protocol;
    private final Map<String, Provider> providers = new LinkedHashMap<>();
    private final Map<String, Task> actions = new LinkedHashMap<>();
    private final Map<String, String> projectDirs = new LinkedHashMap<>();
    private final List<Object> plugins = new ArrayList<>();
    private final ReentrantLock actionLock = new ReentrantLock();
    private boolean stopped;

    Host(PrintStream protocol) {
        this.protocol = protocol;
    }

    void load(String[] jars) throws Exception {
        for (String value : jars) {
            Path jar = Paths.get(value).toAbsolutePath().normalize();
            Manifest manifest = readManifest(jar);

            URL url = jar.toUri().toURL();
            ClassLoader loader = new PluginClassLoader(url, Host.class.getClassLoader());
            Class<?> type = Class.forName(manifest.main(), true, loader);
            if (!Plugin.class.isAssignableFrom(type)) {
                throw new IllegalArgumentException(jar + ": " + manifest.main() + " does not implement dev.kine.plugin.Plugin");
            }
            Plugin plugin = (Plugin) type.getDeclaredConstructor().newInstance();
            plugin.register(new RegistryImpl(manifest.id(), manifest.providers()));

            Map<String, Object> summary = new LinkedHashMap<>();
            summary.put("id", manifest.id());
            summary.put("version", manifest.version());
            summary.put("providers", new ArrayList<>(manifest.providers()));
            plugins.add(summary);
        }
    }

    void serve(InputStream input) throws IOException {
        BufferedReader reader = new BufferedReader(new InputStreamReader(input, StandardCharsets.UTF_8));
        String line;
        while (!stopped && (line = reader.readLine()) != null) {
            if (line.isBlank()) {
                continue;
            }

            Map<String, Object> request = asObject(Json.parse(line), "request");
            Object id = request.get("id");
            if (id == null) {
                continue;
            }
            String method = asString(request.get("method"), "method");
            Map<String, Object> params = new LinkedHashMap<>();
            if (request.get("params") instanceof Map<?, ?> map) {
                params = asObject(map, "params");
            }

            try {
                send(response(id, dispatch(method, params)));
            } catch (RpcException error) {
                send(errorResponse(id, error.code(), error.getMessage()));
            } catch (Exception error) {
                System.err.println("kine plugin host: " + method + ": " + error);
                error.printStackTrace(System.err);
                send(errorResponse(id, -32000, String.valueOf(error.getMessage())));
            }
        }
    }

    private Object dispatch(String method, Map<String, Object> params) throws Exception {
        switch (method) {
            case "handshake":
                return handshake();
            case "tasks":
                return tasks(params);
            case "run":
                run(params);
                return Map.of();
            case "shutdown":
                stopped = true;
                return Map.of();
            default:
                throw new RpcException(-32601, "method not found: " + method);
        }
    }

    private Map<String, Object> handshake() {
        Map<String, Object> result = new LinkedHashMap<>();
        result.put("api", API_VERSION);
        result.put("plugins", plugins);
        return result;
    }

    private Map<String, Object> tasks(Map<String, Object> params) throws Exception {
        String providerId = requiredString(params, "provider");
        Map<String, Object> project = asObject(params.get("project"), "project");
        String projectId = requiredString(project, "id");
        String dir = requiredString(project, "dir");
        Map<String, Object> options = new LinkedHashMap<>();
        if (project.get("options") instanceof Map<?, ?> map) {
            options = asObject(map, "options");
        }

        Provider provider = providers.get(providerId);
        if (provider == null) {
            throw new RpcException(-32602, "unknown provider " + providerId);
        }

        List<Task> defined = provider.tasks(new dev.kine.plugin.Project(projectId, dir, options));
        if (defined == null) {
            throw new RpcException(-32000, "provider " + providerId + " returned null tasks");
        }

        projectDirs.put(projectId, dir);
        List<Object> result = new ArrayList<>();
        Set<String> seen = new LinkedHashSet<>();
        for (Task task : defined) {
            if (task == null) {
                throw new RpcException(-32000, "provider " + providerId + " returned a null task");
            }
            String taskId = task.id();
            if (taskId == null || taskId.isEmpty()) {
                throw new RpcException(-32000, "provider " + providerId + " returned an empty task id");
            }
            if (taskId.indexOf(':') >= 0) {
                throw new RpcException(-32000, "provider " + providerId + ": task id " + taskId + " must not contain ':'");
            }
            if (!seen.add(taskId)) {
                throw new RpcException(-32000, "provider " + providerId + " returned duplicate task id " + taskId);
            }
            if (task.action() == null) {
                throw new RpcException(-32000, "task " + taskId + " has no action");
            }
            actions.put(actionKey(providerId, projectId, taskId), task);
            result.add(taskJson(task));
        }
        Map<String, Object> response = new LinkedHashMap<>();
        response.put("tasks", result);
        return response;
    }

    private void run(Map<String, Object> params) throws Exception {
        String providerId = requiredString(params, "provider");
        String projectId = requiredString(params, "project");
        String taskId = requiredString(params, "task");

        Task task = actions.get(actionKey(providerId, projectId, taskId));
        if (task == null) {
            throw new RpcException(-32602, "unknown task " + taskId);
        }
        String dir = projectDirs.get(projectId);
        if (dir == null) {
            throw new RpcException(-32602, "unknown project " + projectId);
        }

        actionLock.lock();
        try {
            task.action().run(new ActionContext(Paths.get(dir)));
        } catch (Exception error) {
            String message = error.getMessage();
            if (message == null || message.isEmpty()) {
                message = error.getClass().getName();
            }
            throw new RpcException(-32000, "task " + taskId + " failed: " + message);
        } finally {
            actionLock.unlock();
        }
    }

    private Map<String, Object> taskJson(Task task) {
        Map<String, Object> value = new LinkedHashMap<>();
        value.put("id", task.id());
        value.put("deps", task.deps());
        value.put("inputs", paths(task.inputs()));
        value.put("outputs", paths(task.outputs()));
        value.put("key", task.key());
        return value;
    }

    private static List<Object> paths(List<Task.PathSpec> specs) {
        List<Object> result = new ArrayList<>();
        for (Task.PathSpec spec : specs) {
            Map<String, Object> value = new LinkedHashMap<>();
            value.put("path", spec.path());
            value.put("mode", spec.tree() ? "tree" : "file");
            result.add(value);
        }
        return result;
    }

    private Manifest readManifest(Path jar) throws IOException {
        try (ZipFile zip = new ZipFile(jar.toFile())) {
            ZipEntry entry = zip.getEntry(MANIFEST_PATH);
            if (entry == null) {
                throw new IllegalArgumentException(jar + ": missing " + MANIFEST_PATH);
            }
            if (entry.getSize() > MAX_MANIFEST_SIZE) {
                throw new IllegalArgumentException(jar + ": " + MANIFEST_PATH + " exceeds " + MAX_MANIFEST_SIZE + " bytes");
            }
            byte[] data;
            try (InputStream input = zip.getInputStream(entry)) {
                data = input.readNBytes(MAX_MANIFEST_SIZE + 1);
            }
            if (data.length > MAX_MANIFEST_SIZE) {
                throw new IllegalArgumentException(jar + ": " + MANIFEST_PATH + " exceeds " + MAX_MANIFEST_SIZE + " bytes");
            }

            Map<String, Object> value = asObject(
                    Json.parse(new String(data, StandardCharsets.UTF_8)),
                    jar + ": " + MANIFEST_PATH);
            int api = asInt(value.get("api"), "api");
            if (api != API_VERSION) {
                throw new IllegalArgumentException(jar + ": api " + api + ", want " + API_VERSION);
            }
            String id = asString(value.get("id"), "id");
            String version = asString(value.get("version"), "version");
            String main = asString(value.get("main"), "main");
            List<Object> values = asArray(value.get("providers"), "providers");
            List<String> providers = new ArrayList<>();
            for (Object item : values) {
                providers.add(asString(item, "provider"));
            }
            if (providers.isEmpty()) {
                throw new IllegalArgumentException(jar + ": providers is empty");
            }
            return new Manifest(api, id, version, main, providers);
        }
    }

    private void send(Map<String, Object> message) {
        String line = Json.write(message);
        synchronized (protocol) {
            protocol.print(line);
            protocol.print('\n');
            protocol.flush();
        }
    }

    private static Map<String, Object> response(Object id, Object result) {
        Map<String, Object> message = new LinkedHashMap<>();
        message.put("jsonrpc", "2.0");
        message.put("id", id);
        message.put("result", result);
        return message;
    }

    private static Map<String, Object> errorResponse(Object id, int code, String message) {
        Map<String, Object> error = new LinkedHashMap<>();
        error.put("code", code);
        error.put("message", message);

        Map<String, Object> response = new LinkedHashMap<>();
        response.put("jsonrpc", "2.0");
        response.put("id", id);
        response.put("error", error);
        return response;
    }

    private static String actionKey(String provider, String project, String task) {
        return provider + '\u0000' + project + '\u0000' + task;
    }

    private static String requiredString(Map<String, Object> values, String key) {
        Object value = values.get(key);
        if (!(value instanceof String text) || text.isEmpty()) {
            throw new RpcException(-32602, key + " is required");
        }
        return text;
    }

    private static Map<String, Object> asObject(Object value, String context) {
        if (!(value instanceof Map<?, ?> map)) {
            throw new RpcException(-32600, context + ": expected object");
        }
        Map<String, Object> result = new LinkedHashMap<>();
        for (Map.Entry<?, ?> entry : map.entrySet()) {
            result.put(String.valueOf(entry.getKey()), entry.getValue());
        }
        return result;
    }

    private static List<Object> asArray(Object value, String context) {
        if (!(value instanceof List<?> list)) {
            throw new RpcException(-32600, context + ": expected array");
        }
        return new ArrayList<>(list);
    }

    private static String asString(Object value, String context) {
        if (!(value instanceof String text)) {
            throw new RpcException(-32600, context + ": expected string");
        }
        return text;
    }

    private static int asInt(Object value, String context) {
        if (!(value instanceof Number number)) {
            throw new RpcException(-32600, context + ": expected number");
        }
        return number.intValue();
    }

    private record Manifest(int api, String id, String version, String main, List<String> providers) {
    }

    private final class RegistryImpl implements Registry {
        private final String pluginId;
        private final Set<String> declared;

        RegistryImpl(String pluginId, List<String> declared) {
            this.pluginId = pluginId;
            this.declared = new LinkedHashSet<>(declared);
        }

        @Override
        public void provider(String id, Provider provider) {
            if (id == null || !declared.contains(id)) {
                throw new IllegalArgumentException("plugin " + pluginId + ": provider " + id + " is not declared in the manifest");
            }
            if (provider == null) {
                throw new IllegalArgumentException("plugin " + pluginId + ": provider " + id + " is null");
            }
            if (providers.containsKey(id)) {
                throw new IllegalArgumentException("plugin " + pluginId + ": provider " + id + " is already registered");
            }
            providers.put(id, provider);
        }
    }

    private static final class ActionContext implements Context {
        private final Path dir;

        ActionContext(Path dir) {
            this.dir = dir;
        }

        @Override
        public Path dir() {
            return dir;
        }

        @Override
        public void log(String message) {
            System.err.println(message);
        }
    }

    private static final class RpcException extends RuntimeException {
        private final int code;

        RpcException(int code, String message) {
            super(message);
            this.code = code;
        }

        int code() {
            return code;
        }
    }
}
