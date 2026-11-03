package android.webkit;

// The platform constructor is package-private; this test error uses no WebView provider.
public final class TestWebResourceError extends WebResourceError {
    @Override public int getErrorCode() { return WebViewClient.ERROR_HOST_LOOKUP; }
    @Override public CharSequence getDescription() { return "offline"; }
}
