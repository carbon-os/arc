#include "impl.h"
#include "logger.h"

#include <gtk/gtk.h>
#include <string>

namespace browser {

void WebView::run()
{
    logger::Info("window: entering GTK main loop");
    gtk_main();
    logger::Info("window: GTK main loop exited");
}

void WebView::quit()
{
    logger::Info("window: quit called");
    // gtk_main_quit is not thread-safe; marshal via g_idle_add.
    g_idle_add([](gpointer) -> gboolean {
        gtk_main_quit();
        return G_SOURCE_REMOVE;
    }, nullptr);
}

void WebView::set_title(std::string_view title)
{
    logger::Info("window: set_title %.*s", (int)title.size(), title.data());
    gtk_window_set_title(GTK_WINDOW(impl_->window), std::string(title).c_str());
}

void WebView::set_size(int width, int height)
{
    logger::Info("window: set_size %dx%d", width, height);
    gtk_window_resize(GTK_WINDOW(impl_->window), width, height);
}

} // namespace browser