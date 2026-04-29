#pragma once

namespace browser::mac {

inline constexpr const char* k_shim = R"js(
window.ipc = (() => {
    const _listeners = {};

    window._arc_dispatch = (channel, text) => {
        const cb = _listeners[channel];
        if (cb) cb(text);
    };

    window._arc_dispatch_binary = async (channel, token) => {
        try {
            const res = await fetch(
                'ui-ipc://app/-/host/message/' + channel + '/' + token);
            if (!res.ok) return;
            const buf = await res.arrayBuffer();
            const cb  = _listeners[channel];
            if (cb) cb(buf);
        } catch (e) {
            console.error('[arc] ipc fetch error', e);
        }
    };

    return {
        on(channel, cb)  { _listeners[channel] = cb; },
        off(channel)     { delete _listeners[channel]; },

        post(channel, data) {
            if (data instanceof ArrayBuffer || ArrayBuffer.isView(data)) {
                const bytes = data instanceof ArrayBuffer
                    ? new Uint8Array(data)
                    : new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
                fetch('ui-ipc://app/-/js/binary/' + channel, {
                    method: 'POST',
                    body:   bytes
                });
            } else {
                fetch('ui-ipc://app/-/js/text/' + channel, {
                    method: 'POST',
                    body:   String(data)
                });
            }
        }
    };
})();
)js";

} // namespace browser::mac