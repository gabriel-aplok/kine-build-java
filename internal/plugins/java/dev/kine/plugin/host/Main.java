package dev.kine.plugin.host;

import java.io.PrintStream;

/** Entry point for the kine plugin host. */
public final class Main {
    private Main() {
    }

    public static void main(String[] args) {
        PrintStream protocol = System.out;
        System.setOut(System.err);

        Host host = new Host(protocol);
        try {
            host.load(args);
            host.serve(System.in);
        } catch (Throwable error) {
            System.err.println("kine plugin host: " + error);
            error.printStackTrace(System.err);
            System.exit(1);
        }
    }
}
