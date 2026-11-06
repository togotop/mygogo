plugins {
    id("com.android.library")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "dev.mygo"
    compileSdk = 36
    buildToolsVersion = "36.0.0"
    defaultConfig {
        minSdk = 26
        consumerProguardFiles("consumer-rules.pro")
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_21
        targetCompatibility = JavaVersion.VERSION_21
    }
}

kotlin { jvmToolchain(21) }

dependencies {
    implementation("androidx.core:core-ktx:1.16.0")
    implementation("androidx.activity:activity:1.10.1") // back, edge to edge
    implementation("androidx.webkit:webkit:1.14.0") // the web view's document-start scripts
    implementation("com.google.android.material:material:1.13.0") // bottom navigation, toolbars
    // What the app's plugins bring (mygo-plugin.json).
    providers.gradleProperty("mygoPluginDependencies").orNull?.split(',')?.filter { it.isNotEmpty() }?.forEach { implementation(it) }
}
