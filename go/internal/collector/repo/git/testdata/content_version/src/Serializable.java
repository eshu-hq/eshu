package comprehensive.interfaces;

/**
 * Generic serialization interface.
 */
public interface Serializable<T> {
    String serialize(T obj);
    T deserialize(String data);
}
