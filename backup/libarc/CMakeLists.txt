cmake_minimum_required(VERSION 3.22)
project(libarc VERSION 0.1.0 LANGUAGES CXX)

set(CMAKE_CXX_STANDARD          20)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS        OFF)

set(CMAKE_RUNTIME_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/bin")
set(CMAKE_LIBRARY_OUTPUT_DIRECTORY "${CMAKE_BINARY_DIR}/lib")

# ── Platform sources ──────────────────────────────────────────────────────────

set(ARC_COMMON_SOURCES
    src/logger.cpp
)

if(WIN32)

    list(APPEND ARC_COMMON_SOURCES
        src/host_channel_win.cpp
        src/browser/win/webview.cpp
        src/browser/win/window.cpp
        src/browser/win/navigation.cpp
        src/browser/win/ipc.cpp
        src/browser/win/scheme.cpp
        src/browser/win/billing.cpp
    )

elseif(APPLE)

    enable_language(OBJCXX)
    set(CMAKE_OBJCXX_STANDARD          20)
    set(CMAKE_OBJCXX_STANDARD_REQUIRED ON)
    set(CMAKE_OBJCXX_EXTENSIONS        OFF)

    list(APPEND ARC_COMMON_SOURCES
        src/host_channel_unix.cpp
        src/browser/mac/webview.mm
        src/browser/mac/window.mm
        src/browser/mac/navigation.mm
        src/browser/mac/ipc.mm
        src/browser/mac/scheme.mm
        src/browser/mac/billing.mm
    )

else()

    list(APPEND ARC_COMMON_SOURCES
        src/host_channel_unix.cpp
        src/browser/linux/webview.cpp
        src/browser/linux/window.cpp
        src/browser/linux/navigation.cpp
        src/browser/linux/ipc.cpp
        src/browser/linux/scheme.cpp
        src/browser/linux/billing.cpp
    )

endif()

# ── libarc — shared library (production) ─────────────────────────────────────

add_library(libarc SHARED
    ${ARC_COMMON_SOURCES}
    src/arc.cpp
)

set_target_properties(libarc PROPERTIES
    OUTPUT_NAME "arc"
    POSITION_INDEPENDENT_CODE ON
)

target_include_directories(libarc PUBLIC include PRIVATE src)

# ── arc-host — standalone development binary ──────────────────────────────────

add_executable(arc-host
    ${ARC_COMMON_SOURCES}
    src/arc_host_main.cpp
)

target_include_directories(arc-host PRIVATE include src)

# ── Platform linkage ──────────────────────────────────────────────────────────

if(WIN32)

    find_package(unofficial-webview2 CONFIG REQUIRED)

    foreach(_target libarc arc-host)
        target_link_libraries(${_target} PRIVATE
            unofficial::webview2::webview2
            dwmapi
            ole32 oleaut32 shlwapi version)
        target_compile_definitions(${_target} PRIVATE
            WIN32_LEAN_AND_MEAN NOMINMAX UNICODE _UNICODE)
    endforeach()

    set_target_properties(arc-host PROPERTIES WIN32_EXECUTABLE TRUE)
    if(MSVC)
        target_link_options(arc-host PRIVATE /ENTRY:mainCRTStartup)
    endif()

elseif(APPLE)

    find_library(WEBKIT_FRAMEWORK     WebKit     REQUIRED)
    find_library(COCOA_FRAMEWORK      Cocoa      REQUIRED)
    find_library(FOUNDATION_FRAMEWORK Foundation REQUIRED)
    find_library(STOREKIT_FRAMEWORK   StoreKit   REQUIRED)

    foreach(_target libarc arc-host)
        target_link_libraries(${_target} PRIVATE
            ${WEBKIT_FRAMEWORK}
            ${COCOA_FRAMEWORK}
            ${FOUNDATION_FRAMEWORK}
            ${STOREKIT_FRAMEWORK}
        )
        target_compile_options(${_target} PRIVATE -fobjc-arc)
    endforeach()

    set_target_properties(libarc   PROPERTIES MACOSX_BUNDLE FALSE)
    set_target_properties(arc-host PROPERTIES MACOSX_BUNDLE FALSE)

else()

    find_package(PkgConfig REQUIRED)
    pkg_check_modules(WEBKIT2GTK REQUIRED IMPORTED_TARGET webkit2gtk-4.1)
    pkg_check_modules(GTK3       REQUIRED IMPORTED_TARGET gtk+-3.0)

    foreach(_target libarc arc-host)
        target_link_libraries(${_target} PRIVATE
            PkgConfig::WEBKIT2GTK
            PkgConfig::GTK3)
    endforeach()

    pkg_check_modules(JSC QUIET IMPORTED_TARGET javascriptcoregtk-4.1)
    if(JSC_FOUND)
        foreach(_target libarc arc-host)
            target_link_libraries(${_target} PRIVATE PkgConfig::JSC)
        endforeach()
    endif()

    find_package(Threads REQUIRED)
    foreach(_target libarc arc-host)
        target_link_libraries(${_target} PRIVATE Threads::Threads)
    endforeach()

endif()

# ── Warnings ──────────────────────────────────────────────────────────────────

if(MSVC)
    foreach(_target libarc arc-host)
        target_compile_options(${_target} PRIVATE /W4 /WX)
    endforeach()
else()
    foreach(_target libarc arc-host)
        target_compile_options(${_target} PRIVATE
            -Wall -Wextra -Wpedantic -Wno-unused-parameter)
    endforeach()
endif()

# ── Install ───────────────────────────────────────────────────────────────────

install(TARGETS libarc   LIBRARY DESTINATION lib)
install(TARGETS arc-host RUNTIME DESTINATION bin)
install(DIRECTORY include/arc DESTINATION include)