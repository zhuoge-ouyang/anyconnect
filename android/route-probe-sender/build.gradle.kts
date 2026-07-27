plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "com.msitools.anyconnectmobile.routeprobesender"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.msitools.anyconnectmobile.routeprobesender"
        minSdk = 26
        targetSdk = 35
        versionCode = 4
        versionName = "0.1.3-helper-warmup-activity"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    testImplementation(libs.junit)
}
