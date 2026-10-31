import org.gradle.api.tasks.PathSensitivity

plugins {
    id("com.android.application")
}

val androidSdk = System.getenv("ANDROID_HOME")
    ?: System.getenv("ANDROID_SDK_ROOT")
    ?: "${System.getProperty("user.home")}/Library/Android/sdk"
val repositoryRoot = rootProject.projectDir.parentFile
val goBridgeAar = layout.buildDirectory.file("generated/fp-mobile.aar")
val goSources = fileTree(repositoryRoot) {
    include("go.mod", "go.sum")
    include("mobile/**/*.go", "internal/**/*.go", "internal/httpapi/web/**")
}
val buildGoBridge = tasks.register<Exec>("buildGoBridge") {
    inputs.files(goSources).withPathSensitivity(PathSensitivity.RELATIVE)
    inputs.file(rootProject.file("scripts/build-go-bridge.sh"))
    outputs.file(goBridgeAar)
    workingDir(repositoryRoot)
    doFirst { goBridgeAar.get().asFile.parentFile.mkdirs() }
    commandLine("sh", rootProject.file("scripts/build-go-bridge.sh").absolutePath, goBridgeAar.get().asFile.absolutePath)
    environment("ANDROID_HOME", androidSdk)
    environment("ANDROID_SDK_ROOT", androidSdk)
    environment("ANDROID_NDK_HOME", System.getenv("ANDROID_NDK_HOME") ?: "$androidSdk/ndk/30.0.16248370")
}

tasks.named("preBuild") {
    dependsOn(buildGoBridge)
}

dependencies {
    implementation(files(goBridgeAar))
    androidTestImplementation("androidx.test:runner:1.6.2")
    androidTestImplementation("androidx.test.ext:junit:1.2.1")

}

android {
    namespace = "com.fp.flightpicture"
    compileSdk = 37
    buildToolsVersion = "37.0.0"

    defaultConfig {
        applicationId = "com.fp.flightpicture"
        minSdk = 26
        targetSdk = 37
        versionCode = 2
        versionName = "0.1.2"
        testInstrumentationRunner = "androidx.test.runner.AndroidJUnitRunner"
        ndk {
            abiFilters += setOf("arm64-v8a", "armeabi-v7a", "x86", "x86_64")
        }
    }
}
