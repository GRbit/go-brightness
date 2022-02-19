<h3>Tiny script to control brightness and show notifications.</h3>

I had problems with brightness control on Linux with different devices. I used to have a python script for it, but its initialization takes 100-300ms, so I can't hit my hotkey too often. That's why I rewrite it in go.

<h3>How to</h3>

You can run the script in a console like `./main inc` to increase brightness and `./main dec` to decrease it. So just bind it on your desired hotkey combo.

<h3>Dependencies</h3>

1. brightnessctl
2. AMD gpu

The second one isn't really obligatory, just check out your devices with brightnessctl and replace constant on top of the main.go file.
