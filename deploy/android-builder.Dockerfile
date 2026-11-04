FROM eclipse-temurin:17-jdk-jammy
RUN apt-get update && apt-get install -y --no-install-recommends curl unzip ca-certificates && rm -rf /var/lib/apt/lists/*
ENV ANDROID_HOME=/opt/android ANDROID_SDK_ROOT=/opt/android GRADLE_USER_HOME=/gradle-cache
RUN mkdir -p /opt/android/cmdline-tools && curl -fL --retry 3 https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip -o /tmp/android.zip && unzip -q /tmp/android.zip -d /tmp/android && mv /tmp/android/cmdline-tools /opt/android/cmdline-tools/latest && rm /tmp/android.zip
RUN yes | /opt/android/cmdline-tools/latest/bin/sdkmanager --licenses >/dev/null; /opt/android/cmdline-tools/latest/bin/sdkmanager 'platforms;android-35' 'build-tools;35.0.0' 'platform-tools'
RUN curl -fL --retry 3 https://services.gradle.org/distributions/gradle-8.10.2-bin.zip -o /tmp/gradle.zip && curl -fL --retry 3 https://services.gradle.org/distributions/gradle-8.10.2-bin.zip.sha256 -o /tmp/gradle.sha256 && echo "$(cat /tmp/gradle.sha256)  /tmp/gradle.zip" | sha256sum -c - && unzip -q /tmp/gradle.zip -d /opt && rm /tmp/gradle.zip /tmp/gradle.sha256
ENV PATH=/opt/gradle-8.10.2/bin:/opt/android/platform-tools:$PATH
WORKDIR /src
