package comprehensive

import kotlinx.coroutines.*
import kotlinx.coroutines.flow.*

suspend fun fetchData(url: String): String {
    delay(100)
    return "data from $url"
}

suspend fun processAll(urls: List<String>): List<String> = coroutineScope {
    urls.map { url ->
        async { fetchData(url) }
    }.awaitAll()
}

fun numbersFlow(): Flow<Int> = flow {
    for (i in 1..10) {
        delay(50)
        emit(i)
    }
}

class AsyncService {
    private val scope = CoroutineScope(Dispatchers.Default)

    fun startProcessing(items: List<String>) {
        scope.launch {
            items.forEach { item ->
                val result = fetchData(item)
                println("Processed: $result")
            }
        }
    }

    fun cancel() {
        scope.cancel()
    }
}
