package com.manga.reader

import android.os.Bundle
import android.webkit.WebView
import android.webkit.WebViewClient
import android.webkit.WebSettings
import android.view.View
import android.view.WindowManager
import android.util.Log
import androidx.appcompat.app.AppCompatActivity

class MainActivity : AppCompatActivity() {
    private lateinit var webView: WebView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Log.i("MangaReader", "MainActivity.onCreate")

        // Fullscreen
        window.setFlags(
            WindowManager.LayoutParams.FLAG_FULLSCREEN,
            WindowManager.LayoutParams.FLAG_FULLSCREEN
        )

        // Initialize Wails
        System.loadLibrary("wails")
        nativeOnCreate()

        // Create WebView
        webView = WebView(this)
        webView.webViewClient = WailsWebViewClient()
        webView.settings.apply {
            javaScriptEnabled = true
            domStorageEnabled = true
            allowFileAccess = true
            allowContentAccess = true
            mixedContentMode = WebSettings.MIXED_CONTENT_ALWAYS_ALLOW
        }
        webView.addJavascriptInterface(WailsBridge(), "wails")

        setContentView(webView)
    }

    override fun onDestroy() {
        super.onDestroy()
        webView.destroy()
        nativeOnDestroy()
    }

    override fun onBackPressed() {
        if (webView.canGoBack()) {
            webView.goBack()
        } else {
            super.onBackPressed()
        }
    }

    private inner class WailsWebViewClient : WebViewClient() {
        override fun shouldOverrideUrlLoading(view: WebView, url: String): Boolean {
            if (url.startsWith("wails://")) {
                // Handle Wails internal URLs
                return true
            }
            return false
        }
    }

    private inner class WailsBridge {
        @android.webkit.JavascriptInterface
        fun postMessage(message: String) {
            nativePostMessage(message)
        }
    }

    external fun nativeOnCreate()
    external fun nativeOnDestroy()
    external fun nativePostMessage(message: String)
}