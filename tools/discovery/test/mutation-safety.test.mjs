/**
 * mutation-safety.test.mjs
 * 
 * Stage F2: Live Mutation Safety Perimeter Unit Tests
 * 使用 Node 原生 http 模块在本地随机端口 (listen 0) 模拟 Controller 进行安全断言
 */

import test from 'node:test';
import assert from 'node:assert/strict';
import http from 'node:http';
import { runControlledSwitch } from '../scenarios/switch-node.mjs';

function createMockControllerServer(initialNode = 'Node-A') {
  let currentNode = initialNode;
  const requests = [];

  const server = http.createServer((req, res) => {
    const url = new URL(req.url, 'http://127.0.0.1');
    requests.push({ method: req.method, path: url.pathname });

    if (req.method === 'GET' && url.pathname.startsWith('/proxies/')) {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({
        name: 'TestGroup',
        type: 'Selector',
        now: currentNode,
        all: ['Node-A', 'Node-B', 'Node-C']
      }));
      return;
    }

    if (req.method === 'PUT' && url.pathname.startsWith('/proxies/')) {
      let body = '';
      req.on('data', d => body += d);
      req.on('end', () => {
        try {
          const parsed = JSON.parse(body);
          if (server.__failSwitch) {
            // 模拟切换未实际生效 (例如内核仍停留在原节点)
          } else if (server.__failRollback && parsed.name === initialNode) {
            // 模拟回滚未实际生效
          } else {
            currentNode = parsed.name;
          }
          res.writeHead(204);
          res.end();
        } catch {
          res.writeHead(400);
          res.end();
        }
      });
      return;
    }

    res.writeHead(404);
    res.end();
  });

  return new Promise((resolve) => {
    server.listen(0, '127.0.0.1', () => {
      const port = server.address().port;
      resolve({
        server,
        url: `http://127.0.0.1:${port}`,
        requests,
        getCurrentNode: () => currentNode,
        close: () => new Promise(r => server.close(r))
      });
    });
  });
}

test('F2-1: runControlledSwitch without allowLiveMutation must do dry-run and send 0 PUT requests', async () => {
  const mock = await createMockControllerServer('Node-A');
  try {
    const res = await runControlledSwitch({
      controllerUrl: mock.url,
      groupName: 'TestGroup',
      targetNode: 'Node-B',
      allowLiveMutation: false
    });

    assert.equal(res.dryRun, true);
    assert.equal(res.originalNode, 'Node-A');
    assert.equal(mock.getCurrentNode(), 'Node-A', 'Kernel node must NOT change');
    const putRequests = mock.requests.filter(r => r.method === 'PUT');
    assert.equal(putRequests.length, 0, 'Zero PUT requests should be sent in dry-run');
  } finally {
    await mock.close();
  }
});

test('F2-2: runControlledSwitch must fail closed when switch verification fails', async () => {
  const mock = await createMockControllerServer('Node-A');
  mock.server.__failSwitch = true; // 模拟切换失效

  try {
    await assert.rejects(
      async () => {
        await runControlledSwitch({
          controllerUrl: mock.url,
          groupName: 'TestGroup',
          targetNode: 'Node-B',
          allowLiveMutation: true
        });
      },
      /Switch verification failed/
    );
  } finally {
    await mock.close();
  }
});

test('F2-3: runControlledSwitch must fail closed when rollback verification fails', async () => {
  const mock = await createMockControllerServer('Node-A');
  mock.server.__failRollback = true; // 模拟回滚失效

  try {
    await assert.rejects(
      async () => {
        await runControlledSwitch({
          controllerUrl: mock.url,
          groupName: 'TestGroup',
          targetNode: 'Node-B',
          durationSec: 0,
          allowLiveMutation: true
        });
      },
      /Rollback verification failed/
    );
  } finally {
    await mock.close();
  }
});

test('F2-4: runControlledSwitch happy path must verify switch, execute callback, and verify rollback', async () => {
  const mock = await createMockControllerServer('Node-A');
  let callbackExecutedUnderSwitchedNode = false;

  try {
    const res = await runControlledSwitch({
      controllerUrl: mock.url,
      groupName: 'TestGroup',
      targetNode: 'Node-B',
      allowLiveMutation: true,
      onSwitched: async () => {
        callbackExecutedUnderSwitchedNode = (mock.getCurrentNode() === 'Node-B');
      }
    });

    assert.equal(callbackExecutedUnderSwitchedNode, true, 'Callback executed under Node-B');
    assert.equal(res.rollbackVerified, true, 'Rollback verified');
    assert.equal(mock.getCurrentNode(), 'Node-A', 'Restored to Node-A');
  } finally {
    await mock.close();
  }
});

test('F2-5: runConfigPatchExperiment without allowLiveMutation must do dry-run and send 0 PATCH requests', async () => {
  const requests = [];
  const server = http.createServer((req, res) => {
    requests.push({ method: req.method, path: req.url });
    if (req.method === 'GET' && req.url === '/connections') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ uploadTotal: 1000, downloadTotal: 2000, connections: [] }));
      return;
    }
    if (req.method === 'GET' && req.url === '/configs') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ mode: 'rule' }));
      return;
    }
    if (req.method === 'PATCH' && req.url === '/configs') {
      res.writeHead(204);
      res.end();
      return;
    }
    res.writeHead(404);
    res.end();
  });

  const mock = await new Promise(resolve => {
    server.listen(0, '127.0.0.1', () => {
      resolve({
        url: `http://127.0.0.1:${server.address().port}`,
        close: () => new Promise(r => server.close(r))
      });
    });
  });

  try {
    const { runConfigPatchExperiment } = await import('../scenarios/run-config-patch-experiment.mjs');
    const res = await runConfigPatchExperiment({
      controllerUrl: mock.url,
      allowLiveMutation: false
    });

    assert.equal(res.dryRun, true);
    const patchRequests = requests.filter(r => r.method === 'PATCH');
    assert.equal(patchRequests.length, 0, 'Zero PATCH requests must be sent in dry-run');
  } finally {
    await mock.close();
  }
});
