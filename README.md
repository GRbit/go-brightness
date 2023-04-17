<h3>Tiny script to control brightness and show notifications.</h3>

I had problems with brightness control on Linux with different DE environments and hardware drivers. They were 
conflicting and doing not what I wanted or not the way I wanted. I used to have a python script to solve this issue 
calling `brightnessctl`. But its initialization takes 100-300ms, which means  I can't hit the brightness hotkey too 
often, not speaking of golding it. That's why I rewrite it in go.

<h3>How to</h3>

You can run the script in a console like `./main inc` to increase brightness and `./main dec` to decrease it. So just bind it on your desired hotkey combo.

In case you have more than one device with brightness control, you can specify it with adding device name to the 
command, like `./main inc intel_backlight`, or `./main dec intel_backlight`, or `./main amdgpu_bl0 inc`,
or `./main amdgpu_bl0 dec`. As you can see, commands can be in any order.

Brightness is controlled exponentially and it's hardcoded. If you want to change it, welcome to the main.go file.

<h3>Notifications</h3>

Library shows pretty notifications using "github.com/esiqveland/notify" and "github.com/godbus/dbus" packages.
