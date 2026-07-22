import ws from 'k6/ws';
import { check, sleep } from 'k6';

export const options = {
  stages: [
    { duration: '30s', target: 10 },
    { duration: '1m', target: 50 },
    { duration: '30s', target: 0 },
  ],
  thresholds: {
    ws_connecting: ['p(95)<100'],
    ws_msgs_sent: ['rate>1'],
  },
};

const URL = __ENV.WS_URL || 'ws://localhost:8085/ws';
const TOKEN = __ENV.TOKEN || 'test-token';

export default function() {
  const res = ws.connect(URL + '?token=' + TOKEN, null, function(socket) {
    socket.on('open', () => {
      socket.send(JSON.stringify({
        id: 'test-1',
        type: 'subscribe',
        timestamp: new Date().toISOString(),
        data: { room_ids: ['general'] }
      }));
    });

    socket.on('message', (msg) => {
      check(msg, { 'message received': (m) => m !== '' });
    });

    socket.setInterval(() => {
      socket.send(JSON.stringify({
        id: 'msg-' + Date.now(),
        type: 'message',
        timestamp: new Date().toISOString(),
        data: { room_id: 'general', content: 'Test message' }
      }));
    }, 5000);

    socket.setTimeout(() => socket.close(), 60000);
  });

  check(res, { 'status is 101': (r) => r && r.status === 101 });
  sleep(1);
}
