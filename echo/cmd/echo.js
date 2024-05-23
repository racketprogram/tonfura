// 引入 http 模組
const http = require('http');

// 設定伺服器的主機名與埠號
const hostname = '127.0.0.1';
const port = 8080;

// 建立伺服器
const server = http.createServer((req, res) => {
    let body = [];

    // 監聽 data 事件來收集請求的資料
    req.on('data', (chunk) => {
        body.push(chunk);
    }).on('end', () => {
        body = Buffer.concat(body).toString();

        // 設定回應頭
        res.statusCode = 200;
        res.setHeader('Content-Type', 'text/plain');

        // 回應請求的內容
        res.end(`You sent: ${body}`);
    });
});

// 讓伺服器開始監聽
server.listen(port, hostname, () => {
    console.log(`Server running at http://${hostname}:${port}/`);
});
