plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
}

android {
    namespace = "com.msitools.anyconnectmobile"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.msitools.anyconnectmobile"
        minSdk = 26
        targetSdk = 35
        versionCode = 22
        versionName = "0.3.9-netd-dns"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    sourceSets.named("main") {
        val openConnectJava = rootProject.file("../.tmp-openconnect/openconnect/java/src")
        if (openConnectJava.isDirectory) {
            java.srcDir(openConnectJava)
            java.exclude("com/example/**")
        }
    }
}

dependencies {
    testImplementation(libs.junit)
    androidTestImplementation(libs.androidx.junit)
    androidTestImplementation(libs.androidx.runner)
}
