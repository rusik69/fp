import Mobile
import UIKit
import WebKit

@main
final class AppDelegate: UIResponder, UIApplicationDelegate, WKNavigationDelegate {
    private let backendPort: Int = 8080
    var window: UIWindow?
    private var goServer: MobileServer?
    private var localOrigin: URL?

    func application(
        _ application: UIApplication,
        didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil
    ) -> Bool {
        let dataDirectory = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
            .appendingPathComponent("FlightPicture", isDirectory: true)
        do {
            try FileManager.default.createDirectory(
                at: dataDirectory,
                withIntermediateDirectories: true,
                attributes: [.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication]
            )
            guard let server = MobileNewServer(dataDirectory.path, backendPort) else {
                fatalError("Could not create the embedded Go server")
            }
            goServer = server
            var serverError: NSError?
            let address = server.start(&serverError)
            if let serverError {
                throw serverError
            }
            guard let origin = URL(string: address) else {
                fatalError("Embedded Go server returned an invalid URL")
            }
            localOrigin = origin
            showWebView(origin: origin)
        } catch {
            showFailure("Flight Picture could not start its local data service. \(error.localizedDescription)")
        }
        return true
    }

    private func showWebView(origin: URL) {
        let configuration = WKWebViewConfiguration()
        configuration.websiteDataStore = .default()
        let webView = WKWebView(frame: .zero, configuration: configuration)
        webView.navigationDelegate = self
        webView.allowsBackForwardNavigationGestures = true
        webView.load(URLRequest(url: origin))

        let controller = UIViewController()
        controller.view = webView
        installRoot(controller)
    }

    private func showFailure(_ message: String) {
        let label = UILabel()
        label.text = message
        label.textColor = .label
        label.textAlignment = .center
        label.numberOfLines = 0
        label.accessibilityIdentifier = "backend-start-error"
        let controller = UIViewController()
        controller.view.backgroundColor = .systemBackground
        controller.view.addSubview(label)
        label.translatesAutoresizingMaskIntoConstraints = false
        NSLayoutConstraint.activate([
            label.leadingAnchor.constraint(equalTo: controller.view.safeAreaLayoutGuide.leadingAnchor, constant: 24),
            label.trailingAnchor.constraint(equalTo: controller.view.safeAreaLayoutGuide.trailingAnchor, constant: -24),
            label.centerYAnchor.constraint(equalTo: controller.view.centerYAnchor),
        ])
        installRoot(controller)
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
        if let localOrigin,
           url.scheme == localOrigin.scheme,
           url.host == localOrigin.host,
           url.port == localOrigin.port {
            decisionHandler(.allow)
            return
        }
        if url.scheme == "https", navigationAction.navigationType == .linkActivated {
            UIApplication.shared.open(url)
        }
        decisionHandler(.cancel)
    }
}
