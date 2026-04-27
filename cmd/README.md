


# build the renderer first
cd renderer
cmake -B build
cmake --build build
cd ..

# then run the app
go run ./cmd