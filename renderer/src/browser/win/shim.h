#pragma once

namespace browser::win {

inline constexpr const char* k_shim = R"js(
window.ipc = (() => {
    const _listeners = {};

    // host → JS
    window.chrome.webview.addEventListener('message', async (event) => {
        const msg = event.data;
        if (!msg || msg.type !== 'host_ipc_message') return;

        const { channel, text, token } = msg;

        if (text !== undefined) {
            const cb = _listeners[channel];
            if (cb) cb(text);
            return;
        }

        // binary: fetch the slot
        const res = await fetch(`ui-ipc://host/message/${channel}/${token}`);
        if (!res.ok) return;
        const buf = await res.arrayBuffer();
        const cb  = _listeners[channel];
        if (cb) cb(buf);
    });

    return {
        on(channel, cb)  { _listeners[channel] = cb; },
        off(channel)     { delete _listeners[channel]; },

        // JS → host: use postMessage, not fetch
        post(channel, data) {
            if (data instanceof ArrayBuffer || ArrayBuffer.isView(data)) {
                // send binary as a regular array via postMessage
                const bytes = data instanceof ArrayBuffer
                    ? new Uint8Array(data)
                    : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
                window.chrome.webview.postMessage({
                    type: 'ipc_binary',
                    channel,
                    data: Array.from(bytes)
                });
            } else {
                window.chrome.webview.postMessage({
                    type: 'ipc_text',
                    channel,
                    text: String(data)
                });
            }
        }
    };
})();
)js";

} // namespace browser::win