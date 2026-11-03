package ch.lkmc.earnest

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import org.json.JSONArray
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL

internal class Server(base: String, private val token: String? = null) {
    private val origin: String = URI(base.trim().trimEnd('/')).let {
        require(it.scheme == "https" && !it.host.isNullOrBlank() && it.userInfo == null && it.rawQuery == null && it.rawFragment == null && (it.path.isNullOrEmpty() || it.path == "/")) { "Enter an HTTPS server origin" }
        it.toString().trimEnd('/')
    }

    suspend fun request(path: String, body: JSONObject? = null): JSONObject = JSONObject(raw(path, body))

    suspend fun array(path: String): JSONArray {
        val response = raw(path, null)
        return if (response.trim() == "null") JSONArray() else JSONArray(response)
    }

    private suspend fun raw(path: String, body: JSONObject?): String = withContext(Dispatchers.IO) {
        val connection = URL(origin + path).openConnection() as HttpURLConnection
        try {
            connection.instanceFollowRedirects = false
            connection.connectTimeout = 15_000
            connection.readTimeout = 30_000
            connection.setRequestProperty("Accept", "application/json")
            token?.let { connection.setRequestProperty("Authorization", "Bearer $it") }
            if (body != null) {
                connection.requestMethod = "POST"
                connection.doOutput = true
                connection.setRequestProperty("Content-Type", "application/json")
                connection.outputStream.use { it.write(body.toString().toByteArray(Charsets.UTF_8)) }
            }
            val status = connection.responseCode
            if (status !in 200..299) throw IllegalStateException("Server returned HTTP $status")
            connection.inputStream.bufferedReader().use { it.readText() }
        } finally {
            connection.disconnect()
        }
    }
}
