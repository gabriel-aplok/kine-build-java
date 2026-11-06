package dev.kine.plugin.host;

import java.net.URL;
import java.net.URLClassLoader;

/** One classloader per plugin jar. */
final class PluginClassLoader extends URLClassLoader {
    PluginClassLoader(URL jar, ClassLoader host) {
        super(new URL[] {jar}, new ApiParent(host));
    }
}

/** Exposes only the plugin API and the JDK to a plugin classloader. */
final class ApiParent extends ClassLoader {
    ApiParent(ClassLoader parent) {
        super(parent);
    }

    @Override
    protected Class<?> loadClass(String name, boolean resolve) throws ClassNotFoundException {
        if (name.startsWith("dev.kine.plugin.host.")) {
            throw new ClassNotFoundException(name);
        }
        if (name.startsWith("dev.kine.plugin.")
                || name.startsWith("java.")
                || name.startsWith("javax.")
                || name.startsWith("jdk.")
                || name.startsWith("sun.")) {
            return super.loadClass(name, resolve);
        }
        throw new ClassNotFoundException(name);
    }
}
