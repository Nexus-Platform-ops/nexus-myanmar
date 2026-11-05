import Flutter
import UIKit
@main
@objc class AppDelegate: FlutterAppDelegate, FlutterImplicitEngineDelegate {
 private var safetyChannel: FlutterMethodChannel?
 override func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
  NotificationCenter.default.addObserver(self, selector: #selector(captured), name: UIApplication.userDidTakeScreenshotNotification, object: nil)
  return super.application(application, didFinishLaunchingWithOptions: launchOptions)
 }
 func didInitializeImplicitFlutterEngine(_ engineBridge: FlutterImplicitEngineBridge) {
  GeneratedPluginRegistrant.register(with: engineBridge.pluginRegistry)
  if let registrar = engineBridge.pluginRegistry.registrar(forPlugin: "NexusSafety") {
   safetyChannel = FlutterMethodChannel(name: "app.nexus/safety", binaryMessenger: registrar.messenger())
  }
 }
 @objc private func captured() {
  // Best-effort post-capture notification, not screenshot prevention.
  safetyChannel?.invokeMethod("capture", arguments: nil)
 }
}
