import type { DefaultTheme } from "vitepress";

export const sidebar: DefaultTheme.Sidebar = {
  "/get-started/": [
    {
      text: "Introduction",
      items: [
        { text: "What is Apranik", link: "/get-started/overview" },
        { text: "Install and run", link: "/get-started/install" },
        { text: "First configuration", link: "/get-started/first-configuration" },
        { text: "FAQ", link: "/get-started/faq" },
        { text: "Security", link: "/get-started/security" },
      ],
    },
  ],
  "/guides/": [
    { text: "Guides", link: "/guides/" },
    { text: "Run Trojan over TLS", link: "/guides/trojan-over-tls" },
    { text: "Run Trojan behind a CDN", link: "/guides/trojan-over-websocket-cdn" },
    { text: "Run a local proxy", link: "/guides/local-proxy" },
    { text: "Hide the server name", link: "/guides/hide-the-server-name" },
  ],
  "/configuration/": [
    {
      text: "General",
      items: [
        { text: "The config file", link: "/configuration/" },
      ],
    },
    {
      text: "Log",
      collapsed: true,
      items: [{ text: "Where logs go", link: "/configuration/log" }],
    },
    {
      text: "Datasets",
      collapsed: true,
      items: [{ text: "What is a dataset", link: "/configuration/datasets" }],
    },
    {
      text: "DNS",
      collapsed: true,
      items: [
        { text: "How DNS works", link: "/configuration/dns/" },
        { text: "DNS server", link: "/configuration/dns/server" },
        { text: "UDP", link: "/configuration/dns/udp" },
        { text: "TCP", link: "/configuration/dns/tcp" },
        { text: "DoT", link: "/configuration/dns/dot" },
        { text: "DoH", link: "/configuration/dns/doh" },
        { text: "System", link: "/configuration/dns/system" },
        { text: "DNS rule", link: "/configuration/dns/rule" },
      ],
    },
    {
      text: "Inbound",
      collapsed: true,
      items: [
        { text: "What is an inbound", link: "/configuration/inbound/" },
        { text: "Users", link: "/configuration/inbound/users" },
        {
          text: "Built-in",
          collapsed: true,
          items: [{ text: "Mixed", link: "/configuration/inbound/mixed" }],
        },
      ],
    },
    {
      text: "Outbound",
      collapsed: true,
      items: [
        { text: "What is an outbound", link: "/configuration/outbound/" },
        {
          text: "Built-in",
          collapsed: true,
          items: [
            { text: "Freedom", link: "/configuration/outbound/freedom" },
            {
              text: "Groups",
              collapsed: true,
              items: [
                { text: "What is a group", link: "/configuration/outbound/groups" },
                { text: "URLTest", link: "/configuration/outbound/urltest" },
                { text: "Fallback", link: "/configuration/outbound/fallback" },
                { text: "Selector", link: "/configuration/outbound/selector" },
              ],
            },
          ],
        },
      ],
    },
    {
      text: "Routing",
      collapsed: true,
      items: [
        { text: "How routing works", link: "/configuration/routing/" },
        { text: "Rule", link: "/configuration/routing/rule" },
        { text: "Action", link: "/configuration/routing/action" },
        { text: "Sniffing", link: "/configuration/routing/sniffing" },
      ],
    },
    {
      text: "Stats",
      collapsed: true,
      items: [
        { text: "What is counted", link: "/configuration/stats/" },
        { text: "Counters", link: "/configuration/stats/counters" },
      ],
    },
    {
      text: "API",
      collapsed: true,
      items: [
        { text: "What are APIs", link: "/configuration/api/" },
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
          collapsed: true,
          items: [
            { text: "How transports stack", link: "/configuration/transport/" },
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
