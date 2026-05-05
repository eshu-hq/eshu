package comprehensive

interface Identifiable {
    val id: String
}

interface Describable {
    fun describe(): String
}

interface Repository<T : Identifiable> {
    fun findById(id: String): T?
    fun findAll(): List<T>
    fun save(entity: T): T
    fun delete(id: String): Boolean
}

// Interface with default implementation
interface Logger {
    fun log(level: String, message: String) {
        println("[$level] $message")
    }

    fun info(message: String) = log("INFO", message)
    fun warn(message: String) = log("WARN", message)
    fun error(message: String) = log("ERROR", message)
}

// Implementation
data class User(override val id: String, val name: String, val email: String) : Identifiable, Describable {
    override fun describe(): String = "User($name, $email)"
}

class InMemoryRepository<T : Identifiable> : Repository<T>, Logger {
    private val store = mutableMapOf<String, T>()

    override fun findById(id: String): T? {
        info("Finding by id: $id")
        return store[id]
    }

    override fun findAll(): List<T> = store.values.toList()

    override fun save(entity: T): T {
        store[entity.id] = entity
        info("Saved: ${entity.id}")
        return entity
    }

    override fun delete(id: String): Boolean {
        val removed = store.remove(id)
        return removed != null
    }
}
