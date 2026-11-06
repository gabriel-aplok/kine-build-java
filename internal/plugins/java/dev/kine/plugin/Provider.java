package dev.kine.plugin;

import java.util.List;

/** Builds the tasks for one project. */
public interface Provider {
    List<Task> tasks(Project project) throws Exception;
}
