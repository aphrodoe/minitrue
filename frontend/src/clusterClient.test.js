import {
  __resetClusterClientForTests,
  __setKnownNodesForTests,
  fetchFromCluster,
  refreshClusterMembers,
} from './clusterClient';

function jsonResponse(body, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: jest.fn().mockResolvedValue(body),
  };
}

function plainResponse(status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
  };
}

describe('clusterClient', () => {
  let warnSpy;

  beforeEach(() => {
    global.fetch = jest.fn();
    warnSpy = jest.spyOn(console, 'warn').mockImplementation(() => {});
    __resetClusterClientForTests();
  });

  afterEach(() => {
    warnSpy.mockRestore();
    jest.clearAllMocks();
  });

  it('routes requests to nodes discovered from /cluster/members', async () => {
    global.fetch
      .mockResolvedValueOnce(jsonResponse({
        nodes: {
          'node-a': {
            id: 'node-a',
            address: '127.0.0.1',
            http_port: 9090,
            status: 'active',
          },
          'node-b': {
            id: 'node-b',
            address: '127.0.0.1',
            http_port: 9091,
            status: 'active',
          },
        },
      }))
      .mockResolvedValueOnce(plainResponse());

    await refreshClusterMembers();
    await fetchFromCluster('/query', { method: 'POST' });

    expect(global.fetch).toHaveBeenNthCalledWith(1, 'http://localhost:8080/cluster/members', { method: 'GET' });
    expect(global.fetch.mock.calls[1][0]).toBe('http://127.0.0.1:9090/query');
  });

  it('excludes nodes that become down after the next refresh', async () => {
    __setKnownNodesForTests([
      { id: 'node-a', address: '127.0.0.1', http_port: 9001, status: 'active' },
      { id: 'node-b', address: '127.0.0.1', http_port: 9002, status: 'active' },
    ]);

    global.fetch
      .mockResolvedValueOnce(jsonResponse({
        nodes: {
          'node-a': {
            id: 'node-a',
            address: '127.0.0.1',
            http_port: 9001,
            status: 'down',
          },
          'node-b': {
            id: 'node-b',
            address: '127.0.0.1',
            http_port: 9002,
            status: 'active',
          },
        },
      }))
      .mockResolvedValueOnce(plainResponse());

    await refreshClusterMembers();
    await fetchFromCluster('/query');

    expect(global.fetch).toHaveBeenNthCalledWith(1, 'http://127.0.0.1:9001/cluster/members', { method: 'GET' });
    expect(global.fetch.mock.calls[1][0]).toBe('http://127.0.0.1:9002/query');
  });

  it('keeps using the last-known-good nodes when membership refresh fails', async () => {
    __setKnownNodesForTests([
      { id: 'node-a', address: '127.0.0.1', http_port: 9001, status: 'active' },
      { id: 'node-b', address: '127.0.0.1', http_port: 9002, status: 'active' },
    ]);

    global.fetch
      .mockRejectedValueOnce(new Error('node-a unreachable'))
      .mockRejectedValueOnce(new Error('node-b unreachable'))
      .mockResolvedValueOnce(plainResponse());

    await refreshClusterMembers();
    await fetchFromCluster('/query');

    expect(global.fetch.mock.calls[0][0]).toBe('http://127.0.0.1:9001/cluster/members');
    expect(global.fetch.mock.calls[1][0]).toBe('http://127.0.0.1:9002/cluster/members');
    expect(global.fetch.mock.calls[2][0]).toBe('http://127.0.0.1:9001/query');
  });

  it('continues retrying the next known node after a 5xx response', async () => {
    __setKnownNodesForTests([
      { id: 'node-a', address: '127.0.0.1', http_port: 9001, status: 'active' },
      { id: 'node-b', address: '127.0.0.1', http_port: 9002, status: 'active' },
    ]);

    global.fetch
      .mockResolvedValueOnce(plainResponse(503))
      .mockResolvedValueOnce(plainResponse(200));

    const response = await fetchFromCluster('/query');

    expect(response.status).toBe(200);
    expect(global.fetch.mock.calls[0][0]).toBe('http://127.0.0.1:9001/query');
    expect(global.fetch.mock.calls[1][0]).toBe('http://127.0.0.1:9002/query');
  });
});
