#pragma once

namespace browser::gtk {

inline constexpr const char* k_shim = R"js(
window.ipc = (() => {
    const _listeners = {};

    // C++ → JS (text): injected directly via evaluate_javascript
    window._arc_dispatch = (channel, text) => {
        const cb = _listeners[channel];
        if (cb) cb(text);
    };

    // C++ → JS (binary): fetch payload from same-origin slot
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

        // JS → C++: POST to same-origin scheme handler
        post(channel, data) {
            if (data instanceof ArrayBuffer || ArrayBuffer.isView(data)) {
                const body = data instanceof ArrayBuffer ? data : data.buffer;
                fetch('ui-ipc://app/-/js/binary/' + channel,
                      { method: 'POST', body });
            } else {
                fetch('ui-ipc://app/-/js/text/' + channel, {
                    method: 'POST',
                    body: new TextEncoder().encode(String(data))
                });
            }
        }
    };
})();
)js";

} // namespace browser::gtk