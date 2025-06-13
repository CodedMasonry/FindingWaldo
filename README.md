# FindingWaldo

Seeing if I can use a drone to find me in a crowd

```
ffmpeg -re -i test/sample.mp4 -c:v libx264 -c:a aac -f flv rtmp://0.0.0.0:1935/live/hiiii
```

Required dependencies for GoCV to work:

- opencv-cuda
- vtk
- openmpi
- hdf5
