package ch.lkmc.earnest

import ch.lkmc.earnest.core.ApiException
import ch.lkmc.earnest.core.EarnestApi
import ch.lkmc.earnest.core.SavedSearch
import ch.lkmc.earnest.core.SearchFilter
import ch.lkmc.earnest.core.SearchMatch
import ch.lkmc.earnest.core.SearchMode
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.ensureActive
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.serialization.json.jsonObject
import kotlinx.serialization.json.jsonPrimitive
import java.util.concurrent.atomic.AtomicLong
import java.util.concurrent.atomic.AtomicReference
import kotlin.coroutines.coroutineContext

internal data class SearchUiState(
    val query: String = "",
    val filter: SearchFilter = SearchFilter(),
    val submittedQuery: String = "",
    val submittedFilter: SearchFilter = SearchFilter(),
    val matches: List<SearchMatch> = emptyList(),
    val nextCursor: String? = null,
    val status: String? = null,
    val savedSearches: List<SavedSearch> = emptyList(),
    val selectedSavedId: Long? = null,
    val savedName: String = "",
    val embeddingStatus: String = "disabled",
    val loading: Boolean = false,
    val loadingMetadata: Boolean = false,
    val saving: Boolean = false,
    val error: String? = null,
    val metadataError: String? = null,
    val notice: String? = null,
)

/** A request remains bound to the verified account that supplied its token. */
internal data class SearchSession(
    val api: EarnestApi,
    val isCurrent: () -> Boolean,
    val unauthorized: () -> Unit,
)

/**
 * Online search state belongs to the current app session, not the offline
 * snapshot. Epoch checks also fence resets originating from background sync.
 * A new query cancels its predecessor; immutable submitted filters own paging
 * and Save as new even while the reader edits the next query.
 */
internal class SearchModel(
    private val scope: CoroutineScope,
    private val session: () -> SearchSession?,
) {
    private val _state = MutableStateFlow(SearchUiState())
    val state = _state.asStateFlow()
    private val epoch = AtomicLong()
    private val requestId = AtomicLong()
    private val metadataId = AtomicLong()
    private val generation = AtomicReference<String?>()
    @Volatile private var searchJob: Job? = null
    @Volatile private var metadataJob: Job? = null
    @Volatile private var mutationJob: Job? = null

    fun reset() {
        val current = epoch.incrementAndGet()
        searchJob?.cancel()
        metadataJob?.cancel()
        mutationJob?.cancel()
        publish(current) { SearchUiState() }
    }

    fun invalidateForGeneration(current: String) {
        val previous = generation.getAndSet(current)
        if (previous != null && previous != current) reset()
    }

    fun setQuery(query: String) { _state.update { it.copy(query = query) } }
    fun setFilter(filter: SearchFilter) { _state.update { it.copy(filter = filter) } }
    fun setSavedName(name: String) { _state.update { it.copy(savedName = name) } }

    private fun publish(expected: Long, transform: (SearchUiState) -> SearchUiState) {
        _state.update { if (epoch.get() == expected) transform(it) else it }
    }

    private fun current(context: SearchSession, expected: Long): Boolean =
        epoch.get() == expected && context.isCurrent()

    fun loadMetadata() {
        val context = session() ?: return
        val expected = epoch.get()
        val request = metadataId.incrementAndGet()
        metadataJob?.cancel()
        publish(expected) { it.copy(loadingMetadata = true, metadataError = null) }
        metadataJob = scope.launch {
            try {
                if (!current(context, expected)) return@launch
                try {
                    val saved = context.api.savedSearches()
                    coroutineContext.ensureActive()
                    if (!current(context, expected) || metadataId.get() != request) return@launch
                    publish(expected) { it.copy(savedSearches = saved) }
                } catch (error: CancellationException) {
                    throw error
                } catch (error: Exception) {
                    if (!current(context, expected) || metadataId.get() != request) return@launch
                    if (unauthorized(context, error)) return@launch
                    publish(expected) { it.copy(metadataError = failure("Could not load saved searches", error)) }
                }
                try {
                    val pipeline = context.api.pipelineStatus()
                    coroutineContext.ensureActive()
                    if (!current(context, expected) || metadataId.get() != request) return@launch
                    val status = pipeline.jsonObject["embed"]?.jsonPrimitive?.content ?: "disabled"
                    publish(expected) { it.copy(embeddingStatus = status) }
                } catch (error: CancellationException) {
                    throw error
                } catch (error: Exception) {
                    if (!current(context, expected) || metadataId.get() != request) return@launch
                    if (unauthorized(context, error)) return@launch
                    publish(expected) { it.copy(embeddingStatus = "disabled", metadataError = "Could not check AI search availability. Full-text search is still available.") }
                }
            } finally {
                if (current(context, expected) && metadataId.get() == request) {
                    publish(expected) { it.copy(loadingMetadata = false) }
                }
            }
        }
    }

    fun selectSaved(saved: SavedSearch?) {
        if (_state.value.saving) return
        _state.update { it.copy(selectedSavedId = saved?.id, savedName = saved?.name.orEmpty(),
            query = saved?.query ?: it.query, filter = saved?.filters ?: it.filter) }
        if (saved != null) submit()
    }

    fun submit() {
        val before = _state.value
        val query = before.query.trim()
        if (query.isEmpty() || query.toByteArray(Charsets.UTF_8).size > MAX_QUERY_BYTES) {
            _state.update { it.copy(error = "Enter a search query of at most 4,096 bytes.") }
            return
        }
        val filter = if (before.embeddingStatus == "enabled") before.filter else before.filter.copy(mode = SearchMode.FULLTEXT)
        _state.update { it.copy(submittedQuery = query, submittedFilter = filter, matches = emptyList(), nextCursor = null, status = null, notice = null) }
        fetch(null)
    }

    fun loadMore() {
        val before = _state.value
        if (!before.loading && before.nextCursor != null) fetch(before.nextCursor)
    }

    fun useFullText() {
        _state.update { it.copy(filter = it.filter.copy(mode = SearchMode.FULLTEXT)) }
        submit()
    }

    private fun fetch(cursor: String?) {
        val expected = epoch.get()
        val context = session()
        if (context == null) {
            publish(expected) { it.copy(error = "Still signing in; search did not run.") }
            return
        }
        val submitted = _state.value
        val request = requestId.incrementAndGet()
        searchJob?.cancel()
        publish(expected) { it.copy(loading = true, error = null) }
        searchJob = scope.launch {
            try {
                if (!current(context, expected)) return@launch
                val page = context.api.search(submitted.submittedQuery, submitted.submittedFilter, cursor)
                coroutineContext.ensureActive()
                if (!current(context, expected) || requestId.get() != request) return@launch
                val incoming = page.matches.orEmpty()
                if (cursor != null && incoming.isNotEmpty() && page.nextCursor == cursor) {
                    publish(expected) { it.copy(nextCursor = null, error = "The server returned a repeated search cursor. Run the search again.") }
                    return@launch
                }
                val unavailable = page.status == "disabled" || page.status == "paused"
                publish(expected) { it.copy(
                    matches = (it.matches + incoming).distinctBy { match -> match.key },
                    status = page.status?.takeIf { unavailable },
                    nextCursor = page.nextCursor?.takeIf { value -> value.isNotEmpty() && incoming.isNotEmpty() && !unavailable },
                ) }
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                if (current(context, expected) && requestId.get() == request && !unauthorized(context, error)) {
                    publish(expected) { it.copy(error = failure("Could not search", error)) }
                }
            } finally {
                if (current(context, expected) && requestId.get() == request) publish(expected) { it.copy(loading = false) }
            }
        }
    }

    private sealed interface SavedAction {
        data class Create(val name: String, val query: String, val filter: SearchFilter) : SavedAction
        data class Rename(val id: Long, val name: String) : SavedAction
        data class Delete(val id: Long) : SavedAction
    }

    fun saveNew() {
        val before = _state.value
        val name = before.savedName.trim()
        if (name.isEmpty() || before.submittedQuery.isEmpty()) {
            _state.update { it.copy(error = "Enter a name and run a search before saving it.") }
            return
        }
        mutate(SavedAction.Create(name, before.submittedQuery, before.submittedFilter))
    }

    fun rename() {
        val before = _state.value
        val name = before.savedName.trim()
        if (name.isEmpty() || before.selectedSavedId == null) {
            _state.update { it.copy(error = "Enter a saved search name.") }
            return
        }
        mutate(SavedAction.Rename(before.selectedSavedId, name))
    }

    fun delete() { _state.value.selectedSavedId?.let { mutate(SavedAction.Delete(it)) } }

    private fun mutate(action: SavedAction) {
        if (_state.value.saving) return
        val expected = epoch.get()
        val context = session() ?: return
        publish(expected) { it.copy(saving = true, error = null, notice = null) }
        mutationJob = scope.launch {
            try {
                if (!current(context, expected)) return@launch
                when (action) {
                    is SavedAction.Create -> {
                        val saved = context.api.createSavedSearch(action.name, action.query, action.filter)
                        coroutineContext.ensureActive()
                        if (!current(context, expected)) return@launch
                        publish(expected) { it.copy(savedSearches = it.savedSearches.filterNot { item -> item.id == saved.id } + saved, notice = "Search saved.") }
                    }
                    is SavedAction.Rename -> {
                        context.api.renameSavedSearch(action.id, action.name)
                        coroutineContext.ensureActive()
                        if (!current(context, expected)) return@launch
                        publish(expected) { it.copy(savedSearches = it.savedSearches.map { item -> if (item.id == action.id) item.copy(name = action.name) else item }, notice = "Saved search renamed.") }
                    }
                    is SavedAction.Delete -> {
                        context.api.deleteSavedSearch(action.id)
                        coroutineContext.ensureActive()
                        if (!current(context, expected)) return@launch
                        publish(expected) { it.copy(savedSearches = it.savedSearches.filterNot { item -> item.id == action.id },
                            selectedSavedId = if (it.selectedSavedId == action.id) null else it.selectedSavedId,
                            savedName = if (it.selectedSavedId == action.id) "" else it.savedName, notice = "Saved search deleted.") }
                    }
                }
            } catch (error: CancellationException) {
                throw error
            } catch (error: Exception) {
                if (current(context, expected) && !unauthorized(context, error)) {
                    publish(expected) { it.copy(error = failure("Could not save the search change", error)) }
                }
            } finally {
                if (current(context, expected)) publish(expected) { it.copy(saving = false) }
            }
        }
    }

    private fun unauthorized(context: SearchSession, error: Exception): Boolean {
        if (error !is ApiException || !error.isUnauthorized) return false
        context.unauthorized()
        return true
    }

    private fun failure(action: String, error: Exception): String =
        error.message?.takeIf(String::isNotBlank)?.let { "$action: $it" } ?: "$action."

    private companion object { const val MAX_QUERY_BYTES = 4096 }
}
