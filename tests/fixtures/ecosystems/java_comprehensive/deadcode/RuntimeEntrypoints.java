package comprehensive.deadcode;

import java.io.Externalizable;
import java.io.IOException;
import java.io.ObjectInput;
import java.io.ObjectInputStream;
import java.io.ObjectOutput;
import java.io.ObjectOutputStream;

public final class RuntimeEntrypoints {
    public void bootstrap() throws Exception {
        Class.forName("comprehensive.deadcode.PluginImpl");
        ClassLoader loader = Thread.currentThread().getContextClassLoader();
        loader.loadClass("comprehensive.deadcode.PluginImpl");
        PluginImpl.class.getDeclaredMethod("run", String.class);
    }
}

final class PluginImpl {
    void run(String value) {
    }
}

final class SerializationHooks {
    private void readObject(ObjectInputStream in) throws IOException, ClassNotFoundException {
    }

    private void writeObject(ObjectOutputStream out) throws IOException {
    }

    private Object readResolve() {
        return this;
    }

    private Object writeReplace() {
        return this;
    }

    void helper() {
    }
}

final class ExternalizedState implements Externalizable {
    public void readExternal(ObjectInput in) throws IOException, ClassNotFoundException {
    }

    public void writeExternal(ObjectOutput out) throws IOException {
    }
}
