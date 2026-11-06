package demo;

import dev.kine.plugin.Plugin;
import dev.kine.plugin.Provider;
import dev.kine.plugin.Project;
import dev.kine.plugin.Registry;
import dev.kine.plugin.Task;

import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;

public final class DemoPlugin implements Plugin {
    public void register(Registry registry) {
        registry.provider("demo", new Provider() {
            public List<Task> tasks(Project project) {
                return List.of(
                        Task.create("generate")
                                .output("build/greeting.txt")
                                .key("greeting")
                                .action(context -> {
                                    Path output = context.dir().resolve("build/greeting.txt");
                                    Files.createDirectories(output.getParent());
                                    Files.writeString(output, "hello from plugin", StandardCharsets.UTF_8);
                                }));
            }
        });
    }
}
