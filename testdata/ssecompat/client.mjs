import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createTRPCUntypedClient, httpSubscriptionLink } from '@trpc/client';

const frames = JSON.parse(readFileSync(new URL('./frames.json', import.meta.url)));

async function replay(wire) {
  class WireEventSource {
    listeners = new Map();
    closed = false;
    constructor() {
      setTimeout(() => {
        let lastEventId = '';
        for (const block of wire.split('\n\n')) {
          if (!block || this.closed) continue;
          let type = 'message';
          const data = [];
          for (const line of block.split('\n')) {
            if (line.startsWith('event: ')) type = line.slice(7);
            if (line.startsWith('data: ')) data.push(line.slice(6));
            if (line.startsWith('id: ')) lastEventId = line.slice(4);
          }
          for (const fn of this.listeners.get(type) ?? []) {
            try {
              fn({ data: data.join('\n'), lastEventId });
            } catch (error) {
              this.close();
              WireEventSource.onDeliveryError(error);
            }
          }
        }
      }, 0);
    }
    addEventListener(type, fn) {
      this.listeners.set(type, [...this.listeners.get(type) ?? [], fn]);
    }
    close() { this.closed = true; }
  }
  const client = createTRPCUntypedClient({ links: [httpSubscriptionLink({
    url: 'http://example.test/trpc', EventSource: WireEventSource,
  })] });
  return await new Promise((resolve, reject) => {
    const received = [];
    const timeout = setTimeout(() => { subscription.unsubscribe(); reject(new Error('client did not terminate')); }, 2000);
    const finish = (result) => {
      clearTimeout(timeout);
      resolve({ received, ...result });
      queueMicrotask(() => subscription.unsubscribe());
    };
    WireEventSource.onDeliveryError = (error) => finish({ error });
    const subscription = client.subscription('events', undefined, {
      onData(data) { received.push(data); },
      onError(error) { finish({ error }); },
      onConnectionStateChange(state) {
        if (state.state === 'connecting' && state.error) finish({ error: state.error, retrying: true });
      },
      onComplete() { finish({ completed: true }); },
    });
  });
}

// Run every case even if one fails, to identify all incompatible formatter paths.
let failures = 0;
for (const [name, wire] of Object.entries(frames)) {
  try {
    const result = await replay(wire);
    if (name.endsWith('/tracked')) {
      assert.deepEqual(result, { received: [{ id: '42', data: { text: 'hello' } }], completed: true });
    } else if (name.endsWith('/plain')) {
      assert.deepEqual(result, { received: [{ text: 'hello' }], completed: true });
    } else {
      assert.deepEqual(result.received, []);
      const fallback = name.startsWith('bad-formatter/');
      const validation = name.endsWith('/validation') && !fallback;
      assert.equal(result.error?.message, fallback ? 'internal server error' : validation ? 'Access denied' : 'failed to serialize subscription data');
      assert.equal(result.error.shape.code, validation ? -32003 : -32603);
      assert.equal(result.error.data.code, validation ? 'FORBIDDEN' : 'INTERNAL_SERVER_ERROR');
      assert.equal(result.error.data.httpStatus, validation ? 403 : 500);
      assert.equal(result.error.data.path, name.split('/')[1]);
      assert.equal(Boolean(result.retrying), !validation, 'retryable errors must retain tRPC retry behavior');
      if (name.startsWith('custom/') || name.startsWith('bare/')) {
        assert.equal(result.error.data.detail, 'custom');
      }
      if (name.startsWith('bare/')) assert.equal(result.error.shape.error, 'custom-detail');
    }
  } catch (error) {
    failures++;
    console.error(name, error);
  }
}
assert.equal(failures, 0, `${failures} SSE compatibility cases failed`);
