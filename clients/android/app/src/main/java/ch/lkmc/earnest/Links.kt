package ch.lkmc.earnest

import android.net.Uri

internal fun entryId(uri: Uri?): Long? = parseEntryLink(uri?.toString())

internal fun parseEntryLink(value: String?): Long? {
    val match = value?.let { Regex("earnest://entry/([1-9][0-9]*)").matchEntire(it) } ?: return null
    return match.groupValues[1].toLongOrNull()?.takeIf { it > 0 }
}
