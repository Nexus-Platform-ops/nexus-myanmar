package app.nexus.nexus_mobile
import android.os.Bundle
import android.view.WindowManager
import io.flutter.embedding.android.FlutterActivity
class MainActivity : FlutterActivity() {
 override fun onCreate(savedInstanceState: Bundle?) {
  super.onCreate(savedInstanceState)
  // Entire app protected, not a claim that every screenshot attempt is detectable.
  window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
 }
}
