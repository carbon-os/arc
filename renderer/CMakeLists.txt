cmake_minimum_required(VERSION 3.22)
project(renderer VERSION 0.1.0 LANGUAGES CXX)

# ── Language standard ──────────────────────────────────────────────────────────

set(CMAKE_CXX_STANDARD          20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS        OFF)

# ── Output directory ───────────────────────────────────────────────────────────

set(CMAKE_RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/bin")

# ── Target declaration ─────────────────────────────────────────────────────────

add_executable(renderer)

target_sources(renderer PRIVATE
    src/main.cpp
    src/logger.cpp
)

# include/ for public headers, src/ is the root for internal #include "..." paths
target_include_directories(renderer PRIVATE include src)

# ── Platform-specific sources and dependencies ─────────────────────────────────

# ┌─ Windows ────────────────────────────────────────────────────────────────────
if(WIN32)

    target_sources(renderer PRIVATE
        src/host_channel_win.cpp
        src/browser/win/webview.cpp
        src/browser/win/window.cpp
        src/browser/win/navigation.cpp
        src/browser/win/ipc.cpp
        src/browser/win/scheme.cpp
    )

    find_package(unofficial-webview2 CONFIG REQUIRED)
    target_link_libraries(renderer PRIVATE unofficial::webview2::webview2)

    target_link_libraries(renderer PRIVATE
        ole32
        oleaut32
        shlwapi
        version
    )

    set_target_properties(renderer PROPERTIES WIN32_EXECUTABLE TRUE)

    # Tell MSVC to use standard main() instead of WinMain for a WIN32_EXECUTABLE
    if(MSVC)
        target_link_options(renderer PRIVATE /ENTRY:mainCRTStartup)
    endif()

    target_compile_definitions(renderer PRIVATE
        WIN32_LEAN_AND_MEAN
        NOMINMAX
        UNICODE
        _UNICODE
    )

# ┌─ macOS ──────────────────────────────────────────────────────────────────────
elseif(APPLE)

    # Enable Objective-C++ and set standards ONLY for macOS
    enable_language(OBJCXX)
    set(CMAKE_OBJCXX_STANDARD          20)
    set(CMAKE_OBJCXX_STANDARD_REQUIRED ON)
    set(CMAKE_OBJCXX_EXTENSIONS        OFF)

    target_sources(renderer PRIVATE
        src/host_channel_unix.cpp
        src/browser/mac/webview.mm
        src/browser/mac/window.mm
        src/browser/mac/navigation.mm
        src/browser/mac/ipc.mm
        src/browser/mac/scheme.mm
    )

    find_library(WEBKIT_FRAMEWORK     WebKit     REQUIRED)
    find_library(COCOA_FRAMEWORK      Cocoa      REQUIRED)
    find_library(FOUNDATION_FRAMEWORK Foundation REQUIRED)

    target_link_libraries(renderer PRIVATE
        ${WEBKIT_FRAMEWORK}
        ${COCOA_FRAMEWORK}
        ${FOUNDATION_FRAMEWORK}
    )

    set_target_properties(renderer PROPERTIES MACOSX_BUNDLE FALSE)

    target_compile_options(renderer PRIVATE -fobjc-arc)

# ┌─ Linux ──────────────────────────────────────────────────────────────────────
else()

    target_sources(renderer PRIVATE
        src/host_channel_unix.cpp
        src/browser/linux/webview.cpp
        src/browser/linux/window.cpp
        src/browser/linux/navigation.cpp
        src/browser/linux/ipc.cpp
        src/browser/linux/scheme.cpp
    )

    find_package(PkgConfig REQUIRED)

    pkg_check_modules(WEBKIT2GTK REQUIRED IMPORTED_TARGET webkit2gtk-4.1)
    pkg_check_modules(GTK3 REQUIRED IMPORTED_TARGET gtk+-3.0)

    target_link_libraries(renderer PRIVATE
        PkgConfig::WEBKIT2GTK
        PkgConfig::GTK3
    )

    pkg_check_modules(JSC QUIET IMPORTED_TARGET javascriptcoregtk-4.1)
    if(JSC_FOUND)
        target_link_libraries(renderer PRIVATE PkgConfig::JSC)
    endif()

    find_package(Threads REQUIRED)
    target_link_libraries(renderer PRIVATE Threads::Threads)

endif()

# ── Compiler warnings ──────────────────────────────────────────────────────────

if(MSVC)
    target_compile_options(renderer PRIVATE /W4 /WX)
else()
    target_compile_options(renderer PRIVATE
        -Wall
        -Wextra
        -Wpedantic
        -Wno-unused-parameter
    )
endif()

# ── Install ────────────────────────────────────────────────────────────────────

install(TARGETS renderer
    RUNTIME DESTINATION bin
)