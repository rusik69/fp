package com.fp.flightpicture;

import android.content.Context;
import android.webkit.WebView;
import android.widget.FrameLayout;
import android.widget.LinearLayout;

import androidx.test.core.app.ActivityScenario;
import androidx.test.core.app.ApplicationProvider;
import androidx.test.ext.junit.runners.AndroidJUnit4;

import org.junit.After;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertFalse;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.assertTrue;

@RunWith(AndroidJUnit4.class)
public final class MainActivityTest {
    private Context appContext;

    @Before
    public void clearSavedBackend() {
        appContext = ApplicationProvider.getApplicationContext();
        appContext.getSharedPreferences("fp_mobile", Context.MODE_PRIVATE).edit().clear().commit();
    }

    @After
    public void removeSavedBackend() {
        appContext.getSharedPreferences("fp_mobile", Context.MODE_PRIVATE).edit().clear().commit();
    }

    @Test
    public void embeddedHomeLoadsFromLocalGoServer() throws Exception {
        try (ActivityScenario<MainActivity> scenario = ActivityScenario.launch(MainActivity.class)) {
            WebView webView = webViewFrom(scenario);
            assertNotNull("Activity must display its WebView", webView);

            AtomicReference<String> pageTitle = new AtomicReference<>();
            long deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(15);
            while (System.nanoTime() < deadline && !"FP — Pilot weather".equals(pageTitle.get())) {
                CountDownLatch evaluated = new CountDownLatch(1);
                scenario.onActivity(activity -> webView.evaluateJavascript("document.title", value -> {
                    pageTitle.set(value == null ? null : value.substring(1, value.length() - 1));
                    evaluated.countDown();
                }));
                evaluated.await(1, TimeUnit.SECONDS);
                Thread.sleep(100);
            }
            assertEquals("Embedded home page must load from the in-process backend", "FP — Pilot weather", pageTitle.get());
            AtomicReference<String> pageURL = new AtomicReference<>();
            scenario.onActivity(activity -> pageURL.set(webView.getUrl()));
            assertEquals("http://localhost:8080/", pageURL.get());
        }
    }

    @Test
    public void remoteBackendRequiresTrustedHttpsOrigin() {
        assertEquals("https://briefing.example/", MainActivity.normalizeRemoteBackend(" HTTPS://Briefing.Example "));
        assertEquals("https://briefing.example:8443/", MainActivity.normalizeRemoteBackend("https://briefing.example:8443/"));
        assertFalse(accepted("http://briefing.example"));
        assertFalse(accepted("https://user:secret@briefing.example"));
        assertFalse(accepted("https://briefing.example/private"));
        assertFalse(accepted("https://briefing.example/?token=secret"));
        assertFalse(accepted("https://briefing.example/#fragment"));
        assertFalse(accepted("https://briefing.example:0"));
        assertFalse(accepted("https://briefing.example:65536"));
    }

    @Test
    public void selectedRemoteBackendLoadsAtItsHttpsOrigin() {
        try (ActivityScenario<MainActivity> scenario = ActivityScenario.launch(MainActivity.class)) {
            AtomicReference<WebView> webView = new AtomicReference<>();
            scenario.onActivity(activity -> {
                assertTrue(activity.connectToRemoteBackend("https://briefing.example"));
                LinearLayout root = (LinearLayout) ((FrameLayout) activity.findViewById(android.R.id.content)).getChildAt(0);
                webView.set((WebView) root.getChildAt(1));
            });
            AtomicReference<String> pageURL = new AtomicReference<>();
            scenario.onActivity(activity -> pageURL.set(webView.get().getUrl()));
            assertEquals("https://briefing.example/", pageURL.get());
            assertEquals("https://briefing.example/", appContext.getSharedPreferences("fp_mobile", Context.MODE_PRIVATE)
                    .getString("remote_backend", null));
        }
    }

    private static boolean accepted(String url) {
        return MainActivity.normalizeRemoteBackend(url) != null;
    }

    private static WebView webViewFrom(ActivityScenario<MainActivity> scenario) {
        AtomicReference<WebView> webView = new AtomicReference<>();
        scenario.onActivity(activity -> {
            LinearLayout root = (LinearLayout) ((FrameLayout) activity.findViewById(android.R.id.content)).getChildAt(0);
            if (root.getChildCount() > 1 && root.getChildAt(1) instanceof WebView) {
                webView.set((WebView) root.getChildAt(1));
            }
        });
        return webView.get();
    }
}
