package com.fp.flightpicture;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.Intent;
import android.content.SharedPreferences;
import android.graphics.Color;
import android.graphics.Typeface;
import android.net.Uri;
import android.os.Bundle;
import android.os.Build;
import android.text.InputType;
import android.view.Gravity;
import android.view.Window;
import android.view.WindowInsets;
import android.webkit.WebResourceRequest;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.webkit.WebViewClient;
import android.widget.Button;
import android.widget.EditText;
import android.widget.LinearLayout;
import android.widget.TextView;

import com.fp.mobile.mobile.Mobile;
import com.fp.mobile.mobile.Server;

import java.net.URI;
import java.net.URISyntaxException;
import java.util.Locale;

public final class MainActivity extends Activity {
    private static final int BACKEND_PORT = 8080;
    private static final String PREFERENCES = "fp_mobile";
    private static final String REMOTE_BACKEND = "remote_backend";
    private Server goServer;
    private WebView webView;
    private Uri activeOrigin;

    @Override
    protected void onCreate(Bundle savedInstanceState) {
        super.onCreate(savedInstanceState);
        Window window = getWindow();
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
            window.setDecorFitsSystemWindows(true);
        }
        window.setStatusBarColor(Color.rgb(16, 37, 42));
        window.setNavigationBarColor(Color.rgb(16, 37, 42));
        createWebView();
        String savedBackend = preferences().getString(REMOTE_BACKEND, null);
        String remoteBackend = normalizeRemoteBackend(savedBackend);
        if (remoteBackend != null) {
            loadBackend(Uri.parse(remoteBackend));
            return;
        }
        if (savedBackend != null) preferences().edit().remove(REMOTE_BACKEND).apply();
        startLocalBackend();
    }

    private void createWebView() {
        LinearLayout root = new LinearLayout(this);
        root.setOrientation(LinearLayout.VERTICAL);
        root.setBackgroundColor(Color.rgb(16, 37, 42));
        root.setOnApplyWindowInsetsListener((view, insets) -> {
            int topInset;
            int bottomInset;
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                android.graphics.Insets systemBars = insets.getInsets(WindowInsets.Type.systemBars());
                topInset = systemBars.top;
                bottomInset = systemBars.bottom;
            } else {
                topInset = insets.getSystemWindowInsetTop();
                bottomInset = insets.getSystemWindowInsetBottom();
            }
            view.setPadding(0, topInset, 0, bottomInset);
            return insets;
        });

        LinearLayout toolbar = new LinearLayout(this);
        toolbar.setGravity(Gravity.CENTER_VERTICAL);
        toolbar.setPadding(dp(16), 0, dp(8), 0);
        toolbar.setBackgroundColor(Color.rgb(16, 37, 42));
        TextView title = new TextView(this);
        title.setText("Flight Picture");
        title.setTextColor(Color.WHITE);
        title.setTextSize(16);
        title.setTypeface(null, Typeface.BOLD);
        toolbar.addView(title, new LinearLayout.LayoutParams(0, -1, 1));
        Button serverButton = new Button(this);
        serverButton.setText("Server");
        serverButton.setContentDescription("Change FP server");
        serverButton.setOnClickListener(view -> showBackendSettings());
        toolbar.addView(serverButton, new LinearLayout.LayoutParams(-2, -2));
        root.addView(toolbar, new LinearLayout.LayoutParams(-1, dp(52)));

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
        root.addView(webView, new LinearLayout.LayoutParams(-1, 0, 1));
        setContentView(root);
    }

    private SharedPreferences preferences() {
        return getSharedPreferences(PREFERENCES, MODE_PRIVATE);
    }

    private int dp(int value) {
        return Math.round(value * getResources().getDisplayMetrics().density);
    }

    private void startLocalBackend() {
        stopGoServer();
        try {
            goServer = Mobile.newServer(getFilesDir().getAbsolutePath(), BACKEND_PORT);
            String baseURL = goServer.start();
            activeOrigin = Uri.parse(baseURL);
            loadBackend(activeOrigin);
        } catch (Exception error) {
            stopGoServer();
            activeOrigin = null;
            webView.loadDataWithBaseURL(
                    "about:blank",
                    "<meta name='viewport' content='width=device-width'><p>Flight Picture could not start its local Go service. Tap Server to connect to a trusted HTTPS server.</p>",
                    "text/html",
                    "UTF-8",
                    null
            );
        }
    }

    private void loadBackend(Uri origin) {
        activeOrigin = origin;
        webView.loadUrl(origin.toString());
    }

    private void showBackendSettings() {
        EditText input = new EditText(this);
        input.setSingleLine(true);
        input.setInputType(InputType.TYPE_CLASS_TEXT | InputType.TYPE_TEXT_VARIATION_URI);
        input.setHint("https://fp.example");
        input.setText(preferences().getString(REMOTE_BACKEND, ""));
        input.setSelection(input.getText().length());

        AlertDialog dialog = new AlertDialog.Builder(this)
                .setTitle("FP server")
                .setMessage("Connect to a trusted HTTPS FP server. The server must be configured by its administrator. Credentials are not entered or stored in this app.")
                .setView(input)
                .setNegativeButton("Cancel", null)
                .setNeutralButton("Use on-device server", (ignored, which) -> {
                    preferences().edit().remove(REMOTE_BACKEND).apply();
                    startLocalBackend();
                })
                .setPositiveButton("Connect", null)
                .create();
        dialog.setOnShowListener(ignored -> dialog.getButton(AlertDialog.BUTTON_POSITIVE).setOnClickListener(view -> {
            if (!connectToRemoteBackend(input.getText().toString())) {
                input.setError("Use an HTTPS origin only, without a path, query, fragment, or credentials.");
                return;
            }
            dialog.dismiss();
        }));
        dialog.show();
    }

    boolean connectToRemoteBackend(String rawURL) {
        String normalized = normalizeRemoteBackend(rawURL);
        if (normalized == null) return false;
        preferences().edit().putString(REMOTE_BACKEND, normalized).apply();
        stopGoServer();
        loadBackend(Uri.parse(normalized));
        return true;
    }

    static String normalizeRemoteBackend(String rawURL) {
        if (rawURL == null) return null;
        try {
            URI uri = new URI(rawURL.trim());
            String path = uri.getRawPath();
            int port = uri.getPort();
            if (!"https".equalsIgnoreCase(uri.getScheme())
                    || uri.getHost() == null
                    || uri.getRawAuthority() == null
                    || uri.getRawUserInfo() != null
                    || uri.getRawQuery() != null
                    || uri.getRawFragment() != null
                    || (path != null && !path.isEmpty() && !"/".equals(path))
                    || port == 0
                    || port > 65535
                    || uri.getRawAuthority().endsWith(":")) {
                return null;
            }
            return "https://" + uri.getRawAuthority().toLowerCase(Locale.ROOT) + "/";
        } catch (URISyntaxException | IllegalArgumentException error) {
            return null;
        }
    }

    private boolean routeURL(Uri uri) {
        if (activeOrigin != null && sameOrigin(activeOrigin, uri)) return false;
        if ("https".equalsIgnoreCase(uri.getScheme())) {
            try {
                startActivity(new Intent(Intent.ACTION_VIEW, uri));
            } catch (Exception ignored) {
                // Keep the app on its selected backend if no external browser can open the link.
            }
        }
        return true;
    }

    private static boolean sameOrigin(Uri left, Uri right) {
        if (left.getScheme() == null || right.getScheme() == null
                || left.getHost() == null || right.getHost() == null
                || !left.getScheme().equalsIgnoreCase(right.getScheme())
                || !left.getHost().equalsIgnoreCase(right.getHost())) {
            return false;
        }
        return effectivePort(left) == effectivePort(right);
    }

    private static int effectivePort(Uri uri) {
        if (uri.getPort() != -1) return uri.getPort();
        return "https".equalsIgnoreCase(uri.getScheme()) ? 443 : 80;
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
