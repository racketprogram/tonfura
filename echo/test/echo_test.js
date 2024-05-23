const axios = require('axios');
const { performance } = require('perf_hooks');

const concurrency = 30000; // 並發請求數
let success = 0; // 成功請求計數
let failed = 0; // 失敗請求計數

const start = performance.now(); // 記錄開始時間

const requests = Array.from({ length: concurrency }, () => {
  return axios.post('http://localhost:3000/', 'Hello, World!')
    .then(() => {
      success++;
    })
    .catch(() => {
      failed++;
    });
});

Promise.all(requests)
  .then(() => {
    const end = performance.now(); // 記錄結束時間
    const elapsed = end - start; // 計算執行時間
    console.log(`Total time: ${elapsed}ms`);
    console.log(`Success requests: ${success}`);
    console.log(`Failed requests: ${failed}`);
  })
  .catch(error => {
    console.error('An error occurred:', error);
  });
