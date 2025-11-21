const DEFAULT_BOOTSTRAP_NODES = "http://localhost:8080";

// Refresh every 30s: fast enough for newly joined nodes to become usable without
// noticeable UI delay, but slow enough not to add meaningful polling load.
export const CLUSTER_MEMBER_REFRESH_INTERVAL_MS = 30000;

let bootstrapNodes = parseNodeList(
  process.env.REACT_APP_MINITRUE_BOOTSTRAP_NODES || DEFAULT_BOOTSTRAP_NODES,
);
let knownNodes = [...bootstrapNodes];
let hasSuccessfulRefresh = false;
let refreshTimer = null;

function getDefaultHost() {
  if (
    typeof window !== "undefined" &&
    window.location &&
    window.location.hostname
  ) {
    return window.location.hostname;
  }
  return "localhost";
}

function normalizePath(path) {
  return path.startsWith("/") ? path : `/${path}`;
}

function normalizeHost(address) {
  if (!address) {
    return getDefaultHost();
  }

  const withoutProtocol = String(address).replace(/^https?:\/\//, "");
  const host = withoutProtocol.split(":")[0];
  return host || getDefaultHost();
}

function parseNodeEntry(entry) {
  const trimmed = String(entry || "").trim();
  if (!trimmed) {
    return null;
  }

  const urlText = /^https?:\/\//.test(trimmed) ? trimmed : `http://${trimmed}`;
  try {
    const url = new URL(urlText);
    const httpPort = Number(url.port || (url.protocol === "https:" ? 443 : 80));
    if (!Number.isFinite(httpPort) || httpPort <= 0) {
      return null;
    }

    return {
      id: trimmed,
      address: url.hostname || getDefaultHost(),
      http_port: httpPort,
      status: "active",
      protocol: url.protocol.replace(":", "") || "http",
    };
  } catch (error) {
    return null;
  }
}

function parseNodeList(value) {
  const nodes = String(value || "")
    .split(",")
    .map(parseNodeEntry)
    .filter(Boolean);

  return nodes.length > 0 ? nodes : [parseNodeEntry(DEFAULT_BOOTSTRAP_NODES)];
}

function normalizeNode(node) {
  if (typeof node === "string") {
    return parseNodeEntry(node);
  }

  if (!node || typeof node !== "object") {
    return null;
  }

  const httpPort = Number(node.http_port ?? node.HTTPPort ?? node.httpPort);
  if (!Number.isFinite(httpPort) || httpPort <= 0) {
    return null;
  }

  return {
    id:
      node.id ??
      node.ID ??
      `${normalizeHost(node.address ?? node.Address)}:${httpPort}`,
    address: normalizeHost(node.address ?? node.Address),
    http_port: httpPort,
    status: String(node.status ?? node.Status ?? "active").toLowerCase(),
    protocol: String(node.protocol ?? "http").replace(":", ""),
  };
}

function extractMembers(payload) {
  if (Array.isArray(payload)) {
    return payload;
  }

  if (Array.isArray(payload?.nodes)) {
    return payload.nodes;
  }

  if (payload?.nodes && typeof payload.nodes === "object") {
    return Object.values(payload.nodes);
  }

  if (Array.isArray(payload?.Nodes)) {
    return payload.Nodes;
  }

  if (payload?.Nodes && typeof payload.Nodes === "object") {
    return Object.values(payload.Nodes);
  }

  return [];
}

function getCandidateNodes() {
  if (knownNodes.length > 0) {
    return knownNodes;
  }

  if (!hasSuccessfulRefresh) {
    return bootstrapNodes;
  }

  return [];
}

function buildHttpUrl(node, path) {
  const protocol = node.protocol || "http";
  return `${protocol}://${node.address}:${node.http_port}${normalizePath(path)}`;
}

function buildWebSocketUrl(node, path) {
  const wsProtocol = node.protocol === "https" ? "wss" : "ws";
  return `${wsProtocol}://${node.address}:${node.http_port}${normalizePath(path)}`;
}

export async function refreshClusterMembers() {
  const candidates = getCandidateNodes();
  let lastError = null;

  for (const node of candidates) {
    try {
      const response = await fetch(buildHttpUrl(node, "/cluster/members"), {
        method: "GET",
      });

      if (!response.ok) {
        lastError = new Error(
          `Membership refresh from ${node.id} returned ${response.status}`,
        );
        continue;
      }

      const payload = await response.json();
      knownNodes = extractMembers(payload)
        .map(normalizeNode)
        .filter((member) => member && member.status === "active");
      hasSuccessfulRefresh = true;
      return knownNodes;
    } catch (error) {
      lastError = error;
    }
  }

  if (lastError) {
    console.warn(
      "Failed to refresh MiniTrue cluster membership; using last-known-good nodes",
      lastError,
    );
  }
  return knownNodes;
}

export function startClusterMemberRefresh(
  intervalMs = CLUSTER_MEMBER_REFRESH_INTERVAL_MS,
) {
  if (refreshTimer !== null) {
    return;
  }

  refreshClusterMembers();
  refreshTimer = setInterval(refreshClusterMembers, intervalMs);
}

export function stopClusterMemberRefresh() {
  if (refreshTimer !== null) {
    clearInterval(refreshTimer);
    refreshTimer = null;
  }
}

export async function fetchFromCluster(path, options = {}) {
  let lastError = null;
  const candidates = getCandidateNodes();

  for (const node of candidates) {
    try {
      const response = await fetch(buildHttpUrl(node, path), options);

      if (response.status >= 500 && response.status <= 599) {
        lastError = new Error(`Node ${node.id} returned ${response.status}`);
        continue;
      }

      return response;
    } catch (error) {
      lastError = error;
    }
  }

  throw lastError || new Error("No MiniTrue nodes are reachable");
}

export function getClusterWebSocketUrls(path) {
  return getCandidateNodes().map((node) => buildWebSocketUrl(node, path));
}

export function __setKnownNodesForTests(nodes) {
  knownNodes = nodes.map(normalizeNode).filter(Boolean);
  hasSuccessfulRefresh = true;
}

export function __resetClusterClientForTests() {
  stopClusterMemberRefresh();
  bootstrapNodes = parseNodeList(
    process.env.REACT_APP_MINITRUE_BOOTSTRAP_NODES || DEFAULT_BOOTSTRAP_NODES,
  );
  knownNodes = [...bootstrapNodes];
  hasSuccessfulRefresh = false;
}

if (typeof window !== "undefined" && process.env.NODE_ENV !== "test") {
  startClusterMemberRefresh();
}
