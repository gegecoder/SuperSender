# SuperSender

English | [简体中文](README.md)

[SuperSender](https://gegecoder.github.io/SuperSender/)

> A desktop tool for sending network traffic, load testing and message monitoring

SuperSender is built for ops and QA engineers. It combines **Syslog / TCP / UDP** clients (sending) and servers (listening) in a single native desktop app: paced sending, multi-target delivery, message templates and variables, live rate statistics, received-log persistence, port-occupancy probing and more — ready to use without any command line.

- Author: XinGe (<eclipse8@qq.com>)
- Blog: <https://kevin.blog.csdn.net>
- Releases: <https://gitcode.com/qq_23994787/SuperSender/releases>

## Typical workflow

1. Click **+** on the left to create a session, then choose a protocol (Syslog / TCP / UDP) and a mode (client / server).
2. **Server**: select the NIC to bind and the listening port, then start listening. For TCP you can enable *echo back* to test the link in both directions.
3. **Client**: fill in the target IP / port, send encoding, rate and stop condition; enable multi-target sending when running a load test.
4. In **Message source**, pick a template, load a sample file or edit the payload directly (the Syslog client previews the fully wrapped message in real time).
5. After starting, watch the rate chart in **Live statistics**, view / export / persist received messages in the **Received messages** area, and open **Logs** in the top-right corner for runtime details.

## License

This project is open source under the MIT license, for learning and exchange purposes only.
Use it only for authorized operations and testing work, comply with local laws and regulations, and never use it for unauthorized network probing.
