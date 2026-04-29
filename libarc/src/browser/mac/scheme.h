#pragma once

#import <WebKit/WebKit.h>

namespace browser {
    struct WebViewImpl;
}

@interface ArcSchemeHandler : NSObject <WKURLSchemeHandler>
@property (assign) browser::WebViewImpl* impl;
@end