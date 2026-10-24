package com.fp.flightpicture;

import android.app.Activity;
import android.content.Intent;
import android.graphics.Color;
import android.net.Uri;
import android.os.Bundle;
import android.view.Window;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.TextView;

import com.fp.mobile.mobile.Mobile;
import com.fp.mobile.mobile.Server;

public final class MainActivity extends Activity {
    private static final int BACKEND_PORT = 8080;
    private Server goServer;
    private WebView webView;
    private Uri localOrigin;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        Window window = getWindow();
        window.setStatusBarColor(Color.rgb(16, 37, 42));
        window.setNavigationBarColor(Color.rgb(16, 37, 42));
        try {
            goServer = Mobile.newServer(getFilesDir().getAbsolutePath(), BACKEND_PORT);
            String baseURL = goServer.start();
            localOrigin = Uri.parse(baseURL);
            webView = new WebView(this);
            webView.setBackgroundColor(Color.rgb(16, 37, 42));
            WebSettings settings = webView.getSettings();
            settings.setJavaScriptEnabled(true);
            settings.setDomStorageEnabled(true);
            settings.setDatabaseEnabled(true);
            settings.setAllowFileAccess(false);
            settings.setAllowContentAccess(false);
            settings.setSupportZoom(false);
            settings.setMixedContentMode(WebSettings.MIXED_CONTENT_NEVER_ALLOW);
            webView.setWebViewClient(new WebViewClient() {
                @Override
                public boolean shouldOverrideUrlLoading(WebView view, WebResourceRequest request) {
                    return routeURL(request.getUrl());
                }

                @Override
                public boolean shouldOverrideUrlLoading(WebView view, String url) {
                    return routeURL(Uri.parse(url));
                }
            });
            webView.setFocusableInTouchMode(true);
            setContentView(webView);
            webView.loadUrl(baseURL);
        } catch (Exception error) {
            stopGoServer();
            TextView failure = new TextView(this);
            failure.setText("Flight Picture could not start its local Go service. Restart the app and try again.\n\n" + error.getMessage());
            failure.setTextColor(Color.WHITE);
            failure.setTextSize(16);
            failure.setPadding(32, 32, 32, 32);
            failure.setBackgroundColor(Color.rgb(16, 37, 42));
            setContentView(failure);
        }
    }

    private boolean routeURL(Uri uri) {
        if (localOrigin != null
                && "http".equalsIgnoreCase(uri.getScheme())
                && "localhost".equalsIgnoreCase(uri.getHost())
                && uri.getPort() == localOrigin.getPort()) {
            return false;
        }
        if ("https".equalsIgnoreCase(uri.getScheme())) {
            try {
                startActivity(new Intent(Intent.ACTION_VIEW, uri));
            } catch (Exception ignored) {
                // Keep the native app on its local origin when no external browser can open the link.
            }
        }
        return true;
    }

    @Override
    public void onBackPressed() {
        if (webView != null && webView.canGoBack()) {
            webView.goBack();
        } else {
            super.onBackPressed();
        }
    }

    @Override
    protected void onDestroy() {
        if (webView != null) {
            webView.stopLoading();
            webView.destroy();
            webView = null;
        }
        stopGoServer();
        super.onDestroy();
    }

    private void stopGoServer() {
        if (goServer == null) return;
        try {
            goServer.stop();
        } catch (Exception ignored) {
            // The OS will tear down the loopback listener when the app process exits.
        }
        goServer = null;
    }
}
