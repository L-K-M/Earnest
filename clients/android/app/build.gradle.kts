plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.plugin.compose")
    id("org.jetbrains.kotlin.plugin.serialization")
    id("com.google.devtools.ksp")
}

// Release version comes from Gradle properties only. The defaults are
// development placeholders; artifact-producing release tasks must be given
// explicit values via -PearnestVersion / -PearnestVersionCode (or
// gradle.properties). No release version is committed to the repository.
val earnestVersion = providers.gradleProperty("earnestVersion")
    .map { value -> value.trim().also { require(it.isNotEmpty()) { "earnestVersion must not be blank" } } }
    .getOrElse("dev")
val earnestVersionCode = providers.gradleProperty("earnestVersionCode")
    .map { value ->
        val code = value.trim().toIntOrNull()
            ?: throw GradleException("earnestVersionCode must be an integer")
        require(code in 1..2_100_000_000) { "earnestVersionCode must be between 1 and 2100000000" }
        code
    }
    .getOrElse(1)

android {
    namespace = "ch.lkmc.earnest"
    compileSdk = 37
    buildToolsVersion = "36.1.0"

    defaultConfig {
        applicationId = "ch.lkmc.earnest"
        minSdk = 29
        targetSdk = 36
        versionCode = earnestVersionCode
        versionName = earnestVersion
    }

    signingConfigs {
        create("release") {
            // Credentials come from the environment only. No key material or
            // password is stored in the repository or passed on the command line.
            // With no keystore configured the release signing config stays
            // empty; validateReleaseInputs fails artifact-producing tasks first.
            val keystorePath = providers.environmentVariable("EARNEST_ANDROID_KEYSTORE").getOrElse("")
            if (keystorePath.isNotEmpty()) {
                storeFile = project.file(keystorePath)
            }
            storePassword = providers.environmentVariable("EARNEST_ANDROID_STORE_PASSWORD").getOrElse("")
            keyAlias = providers.environmentVariable("EARNEST_ANDROID_KEY_ALIAS").getOrElse("")
            keyPassword = providers.environmentVariable("EARNEST_ANDROID_KEY_PASSWORD").getOrElse("")
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.getByName("release")
            isMinifyEnabled = false
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }
    buildFeatures { compose = true }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_21
        targetCompatibility = JavaVersion.VERSION_21
    }
    testOptions {
        unitTests.isIncludeAndroidResources = true
    }
}

val releaseVersionNameInput = providers.gradleProperty("earnestVersion").orNull?.trim().orEmpty()
val releaseVersionCodeInput = providers.gradleProperty("earnestVersionCode").orNull?.trim().orEmpty()
val releaseKeystoreInput = providers.environmentVariable("EARNEST_ANDROID_KEYSTORE").orNull?.trim().orEmpty()
val releaseStorePasswordInput = providers.environmentVariable("EARNEST_ANDROID_STORE_PASSWORD").orNull.orEmpty()
val releaseKeyAliasInput = providers.environmentVariable("EARNEST_ANDROID_KEY_ALIAS").orNull?.trim().orEmpty()
val releaseKeyPasswordInput = providers.environmentVariable("EARNEST_ANDROID_KEY_PASSWORD").orNull.orEmpty()

val validateReleaseInputs by tasks.registering {
    group = "verification"
    description = "Validates release signing credentials and explicit version inputs before release packaging."
    doLast {
        val problems = mutableListOf<String>()

        val keystore = releaseKeystoreInput
        val storePassword = releaseStorePasswordInput
        val keyAlias = releaseKeyAliasInput
        val keyPassword = releaseKeyPasswordInput

        if (keystore.isEmpty()) problems += "EARNEST_ANDROID_KEYSTORE is required (path to the release keystore)"
        if (storePassword.isEmpty()) problems += "EARNEST_ANDROID_STORE_PASSWORD is required"
        if (keyAlias.isEmpty()) problems += "EARNEST_ANDROID_KEY_ALIAS is required"
        if (keyPassword.isEmpty()) problems += "EARNEST_ANDROID_KEY_PASSWORD is required"
        if (keystore.isNotEmpty() && !project.file(keystore).isFile) {
            problems += "EARNEST_ANDROID_KEYSTORE does not point to an existing key file: $keystore"
        }

        if (releaseVersionNameInput.isEmpty()) {
            problems += "earnestVersion is required and must not be blank"
        } else if (releaseVersionNameInput.equals("dev", ignoreCase = true)) {
            problems += "earnestVersion 'dev' is a development placeholder; provide an explicit release version"
        } else if (!Regex("^\\d+\\.\\d+\\.\\d+(?:-[0-9A-Za-z-]+(?:\\.[0-9A-Za-z-]+)*)?$").matches(releaseVersionNameInput)) {
            problems += "earnestVersion '$releaseVersionNameInput' must be numeric X.Y.Z with an optional semver prerelease"
        }

        if (releaseVersionCodeInput.isEmpty()) {
            problems += "earnestVersionCode is required and must be provided explicitly for release"
        } else {
            val versionCode = releaseVersionCodeInput.toIntOrNull()
            if (versionCode == null) {
                problems += "earnestVersionCode '$releaseVersionCodeInput' must be an integer"
            } else if (versionCode !in 1..2_100_000_000) {
                problems += "earnestVersionCode $versionCode must be between 1 and 2100000000"
            }
        }

        if (problems.isNotEmpty()) {
            throw GradleException(
                "Release inputs are invalid:\n" +
                    problems.joinToString("\n") { "  - $it" } +
                    "\nProvide EARNEST_ANDROID_KEYSTORE, EARNEST_ANDROID_STORE_PASSWORD, " +
                    "EARNEST_ANDROID_KEY_ALIAS and EARNEST_ANDROID_KEY_PASSWORD, plus explicit " +
                    "-PearnestVersion / -PearnestVersionCode."
            )
        }
    }
}

// Artifact-producing release tasks fail fast when credentials or the explicit
// release version are missing. Debug builds and lint are intentionally not
// wired to this validation.
val releaseArtifactTasks = setOf(
    "packageRelease",
    "packageReleaseBundle",
    "signReleaseBundle",
    "validateSigningRelease"
)
tasks.matching { it.name in releaseArtifactTasks }.configureEach {
    dependsOn(validateReleaseInputs)
}

dependencies {
    implementation(project(":core"))

    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("androidx.core:core-ktx:1.19.1")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.11.0")
    implementation("androidx.lifecycle:lifecycle-viewmodel-compose:2.11.0")
    implementation("androidx.work:work-runtime-ktx:2.12.0")
    implementation("androidx.webkit:webkit:1.18.0")
    implementation("androidx.glance:glance-appwidget:1.2.0")

    implementation(platform("androidx.compose:compose-bom:2026.09.00"))
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.material3:material3-adaptive-navigation-suite")
    implementation("androidx.compose.material3.adaptive:adaptive-navigation")
    implementation("androidx.compose.material:material-icons-core")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-tooling-preview")
    debugImplementation("androidx.compose.ui:ui-tooling")
    debugImplementation("androidx.compose.ui:ui-test-manifest")

    implementation("androidx.room:room-runtime:2.8.5")
    implementation("androidx.room:room-ktx:2.8.5")
    ksp("androidx.room:room-compiler:2.8.5")

    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.9.0")
    implementation("com.github.UnifiedPush:android-connector:3.0.10")

    testImplementation("junit:junit:4.13.2")
    testImplementation("com.squareup.okhttp3:okhttp:4.12.0")
    testImplementation("org.jetbrains.kotlinx:kotlinx-coroutines-test:1.10.2")
    testImplementation("androidx.arch.core:core-testing:2.2.0")
    testImplementation("androidx.test:core:1.7.0")
    testImplementation("androidx.compose.ui:ui-test-junit4")
    testImplementation("androidx.room:room-testing:2.8.5")
    testImplementation("org.robolectric:robolectric:4.17")
}
