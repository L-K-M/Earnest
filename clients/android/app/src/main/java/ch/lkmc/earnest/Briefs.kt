package ch.lkmc.earnest

/** Labels a Today brief with its author's name, falling back to the principal ID. */
internal fun briefLabel(name: String?, principalId: Long, value: String): String =
    "${name ?: "Agent $principalId"}: $value"
