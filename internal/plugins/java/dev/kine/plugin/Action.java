package dev.kine.plugin;

/** Work run by one task. */
public interface Action {
    void run(Context context) throws Exception;
}
