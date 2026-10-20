pragma ComponentBehavior: Bound

import QtQuick
import "transition-model.js" as TransitionModel

QtObject {
  id: controller

  property var transitionModel: null
  property string state: ""
  property int captureWatchdogInterval: 0
  property int masterWatchdogInterval: 0
  readonly property alias captureWatchdogRunning: captureTimer.running
  readonly property alias masterWatchdogRunning: masterTimer.running

  signal exitRequested()
  signal coveredReady()

  function armCaptureWatchdog() {
    captureWatchdog.stop();
    captureWatchdogInterval = TransitionModel.watchdogTimeoutMs(transitionModel);
    if (captureWatchdogInterval > 0) {
      captureWatchdog.start();
    }
  }

  function armMasterWatchdog() {
    masterWatchdog.stop();
    masterWatchdogInterval = TransitionModel.watchdogTimeoutMs(transitionModel);
    if (masterWatchdogInterval > 0) {
      masterWatchdog.start();
    }
  }

  function stopWatchdogs() {
    captureWatchdog.stop();
    masterWatchdog.stop();
  }

  function syncState() {
    const nextState = TransitionModel.status(transitionModel);
    if (nextState === state) {
      return;
    }

    state = nextState;
  }

  function initialize(screenNames, targetProfile) {
    transitionModel = TransitionModel.create(screenNames, targetProfile);
    state = "";
    syncState();
    if (state === "aborted") {
      exitRequested();
      return;
    }
    armCaptureWatchdog();
  }

  function captureReady(screenName) {
    const accepted = TransitionModel.capture(transitionModel, screenName);
    syncState();
    if (state === "aborted") {
      stopWatchdogs();
      exitRequested();
      return;
    }
    if (accepted && state === "captured") {
      armCaptureWatchdog();
    }
  }

  function captureFailed(screenName) {
    if (!TransitionModel.captureFailed(transitionModel, screenName)) {
      return false;
    }

    syncState();
    stopWatchdogs();
    exitRequested();
    return true;
  }

  function checkScreens(screenNames) {
    if (TransitionModel.screensMatch(transitionModel, screenNames)) {
      return true;
    }

    syncState();
    stopWatchdogs();
    exitRequested();
    return false;
  }

  function beginTransition() {
    if (!TransitionModel.beginTransition(transitionModel)) {
      return false;
    }

    syncState();
    captureWatchdog.stop();
    armMasterWatchdog();
    return true;
  }

  function coverFramePresented(screenName) {
    const previousState = state;
    const accepted = TransitionModel.coverFramePresented(transitionModel, screenName);
    syncState();
    if (state === "aborted") {
      stopWatchdogs();
      exitRequested();
      return false;
    }
    if (accepted && previousState !== "covered" && state === "covered") {
      coveredReady();
    }
    return accepted;
  }

  function beginIncoming() {
    if (!TransitionModel.beginIncoming(transitionModel)) {
      return false;
    }

    syncState();
    return true;
  }

  function beginSettling() {
    if (!TransitionModel.beginSettling(transitionModel)) {
      return false;
    }

    syncState();
    return true;
  }

  function abort() {
    if (!TransitionModel.abort(transitionModel)) {
      return false;
    }

    syncState();
    stopWatchdogs();
    exitRequested();
    return true;
  }

  function settlingFramePresented(screenName) {
    const accepted = TransitionModel.settlingFramePresented(transitionModel, screenName);
    syncState();
    if (state === "done") {
      stopWatchdogs();
    } else if (state === "aborted") {
      stopWatchdogs();
      exitRequested();
    }
    return accepted;
  }

  function expireCaptureWatchdog() {
    if (!TransitionModel.expireWatchdog(transitionModel)) {
      return false;
    }

    syncState();
    stopWatchdogs();
    exitRequested();
    return true;
  }

  function expireMasterWatchdog() {
    if (!TransitionModel.expireWatchdog(transitionModel)) {
      return false;
    }

    syncState();
    stopWatchdogs();
    exitRequested();
    return true;
  }

  property Timer captureWatchdog: Timer {
    id: captureTimer
    interval: controller.captureWatchdogInterval
    repeat: false
    onTriggered: controller.expireCaptureWatchdog()
  }

  property Timer masterWatchdog: Timer {
    id: masterTimer
    interval: controller.masterWatchdogInterval
    repeat: false
    onTriggered: controller.expireMasterWatchdog()
  }
}
