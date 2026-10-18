import type { DefaultTheme } from "vitepress";

export const sidebar: DefaultTheme.Sidebar = {
  "/get-started/": [
    { text: "What is Apranik", link: "/get-started/overview" },
    { text: "Install and run", link: "/get-started/install" },
    { text: "First configuration", link: "/get-started/first-configuration" },
    { text: "FAQ", link: "/get-started/faq" },
    { text: "Security", link: "/get-started/security" },
  ],
  "/guides/": [
    { text: "Guides", link: "/guides/" },
    { text: "Run Trojan over TLS", link: "/guides/trojan-over-tls" },
    { text: "Run Trojan behind a CDN", link: "/guides/trojan-over-websocket-cdn" },
    { text: "Run a local proxy", link: "/guides/local-proxy" },
    { text: "Hide the server name", link: "/guides/hide-the-server-name" },
  ],
  "/configuration/": [
    { text: "Configuration", link: "/configuration/" },
    { text: "Log", link: "/configuration/log" },
    {
      text: "Inbound",
      link: "/configuration/inbound/",
      collapsed: true,
      items: [
        { text: "Mixed", link: "/configuration/inbound/mixed" },
        { text: "Users", link: "/configuration/inbound/users" },
      ],
    },
    {
      text: "Outbound",
      link: "/configuration/outbound/",
      collapsed: true,
      items: [
        { text: "Freedom", link: "/configuration/outbound/freedom" },
        { text: "URLTest", link: "/configuration/outbound/urltest" },
        { text: "Fallback", link: "/configuration/outbound/fallback" },
        { text: "Selector", link: "/configuration/outbound/selector" },
      ],
    },
    { text: "Datasets", link: "/configuration/datasets" },
    {
      text: "DNS",
      link: "/configuration/dns/",
      collapsed: true,
      items: [
        { text: "DNS server", link: "/configuration/dns/server" },
        { text: "DNS rule", link: "/configuration/dns/rule" },
      ],
    },
    {
      text: "Routing",
      link: "/configuration/routing/",
      collapsed: true,
      items: [
        { text: "Rule", link: "/configuration/routing/rule" },
        { text: "Action", link: "/configuration/routing/action" },
        { text: "Sniffing", link: "/configuration/routing/sniffing" },
      ],
    },
    {
      text: "Stats",
      link: "/configuration/stats/",
      collapsed: true,
      items: [
        { text: "Counters", link: "/configuration/stats/counters" },
      ],
    },
    {
      text: "API",
      link: "/configuration/api/",
      collapsed: true,
      items: [
        { text: "Clash API", link: "/configuration/api/clash" },
        { text: "Clash API endpoints", link: "/configuration/api/clash-endpoints" },
      ],
    },
    {
      text: "Shared",
      collapsed: true,
      items: [
        {
          text: "Transport",
          link: "/configuration/transport/",
          collapsed: true,
          items: [
            { text: "TCP", link: "/configuration/transport/tcp" },
            { text: "UDP", link: "/configuration/transport/udp" },
            { text: "TLS", link: "/configuration/transport/tls" },
            { text: "WebSocket", link: "/configuration/transport/ws" },
            { text: "HTTPUpgrade", link: "/configuration/transport/httpupgrade" },
            { text: "Fragment", link: "/configuration/transport/fragment" },
            { text: "PROXY protocol", link: "/configuration/transport/proxy_protocol" },
            { text: "Client address behind a proxy", link: "/configuration/transport/original_source_ip" },
          ],
        },
        {
          text: "Protocol",
          collapsed: true,
          items: [
            { text: "SOCKS5", link: "/configuration/protocol/socks5" },
            { text: "HTTP", link: "/configuration/protocol/http" },
            { text: "Trojan", link: "/configuration/protocol/trojan" },
            { text: "VLESS", link: "/configuration/protocol/vless" },
          ],
        },
        { text: "Mux.Cool", link: "/configuration/shared/muxcool" },
        { text: "Proxy request timing", link: "/configuration/shared/proxy-request-timing" },
        { text: "Probe", link: "/configuration/shared/probe" },
        { text: "Timeouts", link: "/configuration/shared/timeouts" },
      ],
    },
  ],
  "/concepts/": [
    { text: "Architecture", link: "/concepts/architecture" },
    { text: "Request lifecycle", link: "/concepts/request-lifecycle" },
    { text: "Mux.Cool on the wire", link: "/concepts/mux-cool" },
    { text: "Glossary", link: "/concepts/glossary" },
  ],
  "/contribute/": [
    { text: "Contribute", link: "/contribute/" },
    { text: "Contribute safely", link: "/contribute/safety" },
    { text: "Your first module", link: "/contribute/first-module" },
    {
      text: "Add a module",
      collapsed: true,
      items: [
        { text: "Base stream transport", link: "/contribute/base-transport" },
        { text: "Overlay transport", link: "/contribute/overlay-transport" },
        { text: "Proxy protocol", link: "/contribute/proxy-protocol" },
        { text: "Sniffer", link: "/contribute/sniffer" },
        { text: "DNS server", link: "/contribute/dns-server" },
        { text: "API module", link: "/contribute/api-module" },
      ],
    },
    { text: "Tests", link: "/contribute/tests" },
    { text: "Pull requests", link: "/contribute/pull-requests" },
    { text: "Practices", link: "/contribute/practices" },
    {
      text: "Performance",
      collapsed: true,
      items: [
        { text: "Performance guide", link: "/contribute/performance" },
        { text: "Performance log", link: "/contribute/performance-log" },
      ],
    },
    { text: "Good first issues", link: "/contribute/good-first-issues" },
    { text: "Editing the docs", link: "/contribute/docs" },
  ],
};

export const navigation: DefaultTheme.NavItem[] = [
  { text: "Get started", link: "/get-started/overview", activeMatch: "/get-started/" },
  { text: "Guides", link: "/guides/", activeMatch: "/guides/" },
  { text: "Configuration", link: "/configuration/", activeMatch: "/configuration/" },
  { text: "Concepts", link: "/concepts/architecture", activeMatch: "/concepts/" },
  { text: "Contribute", link: "/contribute/", activeMatch: "/contribute/" },
];
