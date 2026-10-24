package com.fp.flightpicture;

import android.webkit.WebView;
import android.widget.FrameLayout;

import androidx.test.core.app.ActivityScenario;
import androidx.test.ext.junit.runners.AndroidJUnit4;

import org.junit.Test;
import org.junit.runner.RunWith;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.Assert.assertEquals;
import static org.junit.Assert.assertNotNull;

@RunWith(AndroidJUnit4.class)
public final class MainActivityTest {
    @Test
    public void embeddedHomeLoadsFromLocalGoServer() throws Exception {
        try (ActivityScenario<MainActivity> scenario = ActivityScenario.launch(MainActivity.class)) {
            AtomicReference<WebView> webViewReference = new AtomicReference<>();
            scenario.onActivity(activity -> {
                FrameLayout content = activity.findViewById(android.R.id.content);
                if (content.getChildCount() > 0 && content.getChildAt(0) instanceof WebView) {
                    webViewReference.set((WebView) content.getChildAt(0));
                }
            });
            WebView webView = webViewReference.get();
            assertNotNull("Activity must display its local WebView", webView);

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
}
