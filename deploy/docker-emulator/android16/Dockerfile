ARG BASE_IMAGE=alcor-df-build/docker-android-base:v3.5.2-p0
FROM ${BASE_IMAGE}

USER root

ENV EMULATOR_ANDROID_VERSION=16.0 \
    EMULATOR_API_LEVEL=36 \
    EMULATOR_SYS_IMG=x86_64 \
    EMULATOR_IMG_TYPE=google_apis \
    EMULATOR_BROWSER=chrome \
    EMULATOR_HEADLESS=true \
    EMULATOR_DATA_PARTITION=4g \
    EMULATOR_ADDITIONAL_ARGS="-no-window -no-audio -no-boot-anim" \
    PATH=${PATH}:${ANDROID_HOME}/build-tools

RUN yes | sdkmanager --licenses >/dev/null \
 && sdkmanager \
      "platforms;android-${EMULATOR_API_LEVEL}" \
      "system-images;android-${EMULATOR_API_LEVEL};${EMULATOR_IMG_TYPE};${EMULATOR_SYS_IMG}" \
      "emulator" \
 && ln -sf ${ANDROID_HOME}/emulator/emulator /usr/bin/emulator \
 && apt-get -qqy update \
 && apt-get -qqy install --no-install-recommends \
      libasound2t64 \
      libpulse0 \
 && apt-get clean \
 && rm -rf /var/lib/apt/lists/*

RUN appium driver install uiautomator2@8.2.2 \
 && appium driver list --installed

RUN rm -rf ${SCRIPT_PATH}
ENV SCRIPT_PATH=docker-android \
    APP_PATH=${WORK_PATH}/docker-android \
    DEVICE_TYPE=emulator \
    LOG_PATH=${WORK_PATH}/logs \
    WEB_LOG_PORT=9000 \
    WEB_LOG=false \
    WEB_VNC=false \
    USER_BEHAVIOR_ANALYTICS=false

RUN mkdir -p ${APP_PATH} ${LOG_PATH}
COPY mixins ${APP_PATH}/mixins
COPY cli ${APP_PATH}/cli
RUN find ${APP_PATH}/mixins \( -name '*.sh' -o -name '*.conf' \) -type f -exec sed -i 's/\r$//' {} + \
 && find ${APP_PATH}/mixins/scripts -type f -name '*.sh' -exec chmod 0755 {} + \
 && pip install --break-system-packages --no-cache-dir click==8.4.2 requests==2.34.2 \
 && printf '#!/bin/sh\nexport PYTHONPATH=%s/cli/src\nexec python3 %s/cli/src/app.py "$@"\n' "${APP_PATH}" "${APP_PATH}" > /usr/local/bin/docker-android \
 && chmod 0755 /usr/local/bin/docker-android \
 && echo "${USERNAME} ALL=(ALL) NOPASSWD: ALL" >> /etc/sudoers

USER root
RUN mkdir -p "${WORK_PATH}/.config/Android Open Source Project" \
 && printf '[General]\nshowNestedWarning=false\n' > "${WORK_PATH}/.config/Android Open Source Project/Emulator.conf"

EXPOSE 4723 5554 5555 9000
STOPSIGNAL SIGTERM
CMD ["${APP_PATH}/mixins/scripts/run.sh"]
