import Mobile
import UIKit
import WebKit

@main
final class AppDelegate: UIResponder, UIApplicationDelegate, WKNavigationDelegate {
    private let backendPort: Int = 8080
    private let remoteBackendKey = "remote_backend"
    var window: UIWindow?
    private var goServer: MobileServer?
    private var webView: WKWebView?
    private var activeOrigin: URL?

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        createWebView()
        let defaults = UserDefaults.standard
        if let saved = defaults.string(forKey: remoteBackendKey),
           let remote = Self.normalizedRemoteBackend(saved) {
            defaults.set(remote.absoluteString, forKey: remoteBackendKey)
            loadBackend(remote)
        } else {
            defaults.removeObject(forKey: remoteBackendKey)
            startLocalBackend()
        }
        return true
    }

    private func createWebView() {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .default()
        let webView = WKWebView(frame: .zero, configuration: configuration)
        webView.navigationDelegate = self
        webView.allowsBackForwardNavigationGestures = true
        self.webView = webView

        let controller = UIViewController()
        controller.view = webView
        controller.title = "Flight Picture"
        controller.navigationItem.rightBarButtonItem = UIBarButtonItem(
            title: "Server",
            primaryAction: UIAction { [weak self] _ in self?.showBackendSettings() }
        )
        installRoot(UINavigationController(rootViewController: controller))
    }

    private func startLocalBackend() {
        stopGoServer()
        let dataDirectory = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("FlightPicture", isDirectory: true)
        do {
            try FileManager.default.createDirectory(
                at: dataDirectory,
                withIntermediateDirectories: true,
                attributes: [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication]
            )
            guard let server = MobileNewServer(dataDirectory.path, backendPort) else {
                throw NSError(
                    domain: "FlightPicture",
                    code: 1,
                    userInfo: [NSLocalizedDescriptionKey: "Could not create the embedded Go server"]
                )
            }
            goServer = server
            var serverError: NSError?
            let address = server.start(&serverError)
            if let serverError {
                throw serverError
            }
            guard let origin = URL(string: address) else {
                throw NSError(
                    domain: "FlightPicture",
                    code: 2,
                    userInfo: [NSLocalizedDescriptionKey: "Embedded Go server returned an invalid URL"]
                )
            }
            loadBackend(origin)
        } catch {
            stopGoServer()
            activeOrigin = nil
            showFailure("Flight Picture could not start its local data service. You can use Server to connect to a trusted HTTPS server.")
        }
    }

    private func loadBackend(_ origin: URL) {
        activeOrigin = origin
        webView?.load(URLRequest(url: origin))
    }

    private func showBackendSettings() {
        showBackendSettings(prefilled: UserDefaults.standard.string(forKey: remoteBackendKey) ?? "")
    }

    private func showBackendSettings(prefilled: String) {
        let alert = UIAlertController(
            title: "FP server",
            message: "Connect to a trusted HTTPS FP server configured by its administrator. Credentials are not entered or stored in this app.",
            preferredStyle: .alert
        )
        alert.addTextField { field in
            field.text = prefilled
            field.placeholder = "https://fp.example"
            field.keyboardType = .URL
            field.autocapitalizationType = .none
            field.autocorrectionType = .no
            field.clearButtonMode = .whileEditing
        }
        alert.addAction(UIAlertAction(title: "Cancel", style: .cancel))
        alert.addAction(UIAlertAction(title: "Use on-device", style: .destructive) { [weak self] _ in
            UserDefaults.standard.removeObject(forKey: self?.remoteBackendKey ?? "remote_backend")
            self?.startLocalBackend()
        })
        alert.addAction(UIAlertAction(title: "Connect", style: .default) { [weak self, weak alert] _ in
            let raw = alert?.textFields?.first?.text ?? ""
            guard let origin = Self.normalizedRemoteBackend(raw) else {
                let invalid = UIAlertController(
                    title: "Invalid server address",
                    message: "Enter an HTTPS origin without a path, query, fragment, or credentials.",
                    preferredStyle: .alert
                )
                invalid.addAction(UIAlertAction(title: "OK", style: .default) { [weak self] _ in
                    self?.showBackendSettings(prefilled: raw)
                })
                self?.presentAlert(invalid)
                return
            }
            self?.connectToRemoteBackend(origin)
        })
        presentAlert(alert)
    }

    private func presentAlert(_ alert: UIAlertController) {
        let presenter = window?.rootViewController
        presenter?.present(alert, animated: true)
    }

    private func connectToRemoteBackend(_ origin: URL) {
        UserDefaults.standard.set(origin.absoluteString, forKey: remoteBackendKey)
        stopGoServer()
        loadBackend(origin)
    }

    static func normalizedRemoteBackend(_ raw: String) -> URL? {
        let trimmed = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard var components = URLComponents(string: trimmed),
              components.scheme?.lowercased() == "https",
              let host = components.host,
              !host.isEmpty,
              components.user == nil,
              components.password == nil,
              components.query == nil,
              components.fragment == nil,
              components.path.isEmpty || components.path == "/" else {
            return nil
        }
        if let port = components.port, !(1...65535).contains(port) {
            return nil
        }
        guard let authorityStart = trimmed.range(of: "://")?.upperBound else {
            return nil
        }
        let authority = trimmed[authorityStart...].prefix { $0 != "/" && $0 != "?" && $0 != "#" }
        guard !authority.hasSuffix(":") else {
            return nil
        }
        components.scheme = "https"
        components.host = host.lowercased()
        components.path = "/"
        return components.url
    }

    private func showFailure(_ message: String) {
        let alert = UIAlertController(title: "FP server unavailable", message: message, preferredStyle: .alert)
        alert.addAction(UIAlertAction(title: "Server settings", style: .default) { [weak self] _ in
            self?.showBackendSettings()
        })
        alert.addAction(UIAlertAction(title: "OK", style: .cancel))
        presentAlert(alert)
    }

    private func installRoot(_ controller: UIViewController) {
        let window = UIWindow(frame: UIScreen.main.bounds)
        window.rootViewController = controller
        window.makeKeyAndVisible()
        self.window = window
    }

    func webView(
        _ webView: WKWebView,
        decidePolicyFor navigationAction: WKNavigationAction,
        decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
    ) {
        guard let url = navigationAction.request.url else {
            decisionHandler(.cancel)
            return
        }
        if let activeOrigin, Self.sameOrigin(activeOrigin, url) {
            decisionHandler(.allow)
            return
        }
        if url.scheme?.lowercased() == "https", navigationAction.navigationType == .linkActivated {
            UIApplication.shared.open(url)
        }
        decisionHandler(.cancel)
    }

    private static func sameOrigin(_ lhs: URL, _ rhs: URL) -> Bool {
        guard lhs.scheme?.lowercased() == rhs.scheme?.lowercased(),
              lhs.host?.lowercased() == rhs.host?.lowercased() else {
            return false
        }
        return effectivePort(lhs) == effectivePort(rhs)
    }

    private static func effectivePort(_ url: URL) -> Int {
        if let port = url.port { return port }
        return url.scheme?.lowercased() == "https" ? 443 : 80
    }

    func applicationWillTerminate(_ application: UIApplication) {
        stopGoServer()
    }

    private func stopGoServer() {
        guard let goServer else { return }
        do {
            try goServer.stop()
        } catch {
            NSLog("Embedded Go server shutdown failed: %@", error.localizedDescription)
        }
        self.goServer = nil
    }
}
