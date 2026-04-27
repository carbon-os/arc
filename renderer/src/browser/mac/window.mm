#include "impl.h"
#include "logger.h"

#import <Cocoa/Cocoa.h>

#include <string>

namespace browser {

void WebView::run()
{
    logger::Info("window: entering NSApp run loop");
    [NSApp run];
    logger::Info("window: NSApp run loop exited");
}

void WebView::quit()
{
    logger::Info("window: quit called");
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp stop:nil];
        NSEvent* ev = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                         location:NSZeroPoint
                                    modifierFlags:0
                                        timestamp:0
                                     windowNumber:0
                                          context:nil
                                          subtype:0
                                            data1:0
                                            data2:0];
        [NSApp postEvent:ev atStart:YES];
    });
}

void WebView::set_title(std::string_view title)
{
    logger::Info("window: set_title %.*s", (int)title.size(), title.data());
    [impl_->window setTitle:[NSString stringWithUTF8String:std::string(title).c_str()]];
}

void WebView::set_size(int width, int height)
{
    logger::Info("window: set_size %dx%d", width, height);
    [impl_->window setContentSize:NSMakeSize(static_cast<CGFloat>(width),
                                             static_cast<CGFloat>(height))];
}

} // namespace browser