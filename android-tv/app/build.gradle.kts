plugins { id("com.android.application"); id("org.jetbrains.kotlin.android") }
android {
    namespace = "local.camerahub.tv"
    compileSdk = 35
    buildToolsVersion = "35.0.0"
    defaultConfig {
        applicationId = "local.camerahub.tv"
        minSdk = 26
        targetSdk = 35
        versionCode = 7
        versionName = "1.3.1"
    }
    signingConfigs {
        create("personal") {
            storeFile = file("/keys/camerahub-tv.p12")
            storePassword = System.getenv("KEYSTORE_PASSWORD")
            keyAlias = "camerahub"
            keyPassword = System.getenv("KEYSTORE_PASSWORD")
        }
    }
    buildTypes {
        getByName("release") { signingConfig = signingConfigs.getByName("personal"); isMinifyEnabled = false }
    }
    compileOptions { sourceCompatibility = JavaVersion.VERSION_17; targetCompatibility = JavaVersion.VERSION_17 }
    kotlinOptions { jvmTarget = "17" }
    testOptions {
        unitTests.isIncludeAndroidResources = true
        unitTests.all {
            it.maxHeapSize = "1024m"
            it.systemProperty("maven.repo.local", "${gradle.gradleUserHomeDir}/robolectric-m2")
            it.systemProperty("robolectric.dependency.repo.url", "https://repo.maven.apache.org/maven2")
        }
    }
}
dependencies {
    implementation("androidx.media3:media3-exoplayer:1.6.1")
    implementation("androidx.media3:media3-exoplayer-hls:1.6.1")
    implementation("androidx.media3:media3-ui:1.6.1")
    testImplementation("junit:junit:4.13.2")
    testImplementation("org.robolectric:robolectric:4.14.1")
}
