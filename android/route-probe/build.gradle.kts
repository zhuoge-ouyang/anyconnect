import java.security.MessageDigest

plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
}

val expectedSha = "00CFDE18AFC0C0C7B5E46DF143BD47141F7F22A83EEE4DF24C722806ADDE3E89"
val expectedRecords = 10_826
val sourceIpDb = rootProject.file("../bin/data/china_ip_list.txt")
val generatedAssets = layout.buildDirectory.dir("generated/routeProbeAssets")

android {
    namespace = "com.msitools.anyconnectmobile.routeprobe"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.msitools.anyconnectmobile.routeprobe"
        minSdk = 26
        targetSdk = 35
        versionCode = 15
        versionName = "0.1.14-helper-capture-retry-gate"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        buildConfigField("String", "IPDB_SHA256", "\"$expectedSha\"")
        buildConfigField("int", "IPDB_RECORD_COUNT", expectedRecords.toString())
    }
    buildFeatures {
        buildConfig = true
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
    sourceSets.named("main") {
        assets.srcDir(generatedAssets)
    }
}

val prepareProbeData = tasks.register("prepareProbeData") {
    inputs.file(sourceIpDb)
    inputs.property("expectedSha", expectedSha)
    inputs.property("expectedRecords", expectedRecords)
    outputs.dir(generatedAssets)
    doLast {
        val inputFile = inputs.files.singleFile
        val expectedShaValue = inputs.properties.getValue("expectedSha") as String
        val expectedRecordsValue = inputs.properties.getValue("expectedRecords") as Int
        check(inputFile.isFile) {
            "Required Gate 0 snapshot is missing: " + inputFile.absolutePath
        }
        val bytes = inputFile.readBytes()
        val actualSha = MessageDigest.getInstance("SHA-256")
            .digest(bytes)
            .joinToString("") { "%02X".format(it.toInt() and 0xff) }
        check(actualSha == expectedShaValue) {
            "Gate 0 IPDB SHA-256 changed: expected=$expectedShaValue actual=$actualSha"
        }
        val recordCount = bytes.toString(Charsets.UTF_8)
            .lineSequence()
            .map(String::trim)
            .count { it.isNotEmpty() && !it.startsWith("#") }
        check(recordCount == expectedRecordsValue) {
            "Gate 0 IPDB record count changed: expected=$expectedRecordsValue actual=$recordCount"
        }
        val outDir = outputs.files.singleFile
        outDir.deleteRecursively()
        outDir.mkdirs()
        inputFile.copyTo(outDir.resolve("china_ip_list.txt"), overwrite = true)
        println("Prepared Gate 0 IPDB: records=$recordCount sha256=$actualSha")
    }
}

tasks.named("preBuild").configure {
    dependsOn(prepareProbeData)
}

dependencies {
    testImplementation(libs.junit)
    testImplementation(libs.json.test)
    androidTestImplementation(libs.androidx.junit)
    androidTestImplementation(libs.androidx.runner)
}
