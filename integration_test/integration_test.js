const axios = require('axios');

const sendPostRequest = async (url, body) => {
    try {
        const response = await axios.post(url, body, {
            headers: { 'Content-Type': 'application/json' }
        });
        return response;
    } catch (error) {
        return error.response;
    }
};

const runTest = async () => {
    const startHour = parseInt(process.env.START_HOUR, 10);
    const startMinute = parseInt(process.env.START_MINUTE, 10);
    const baseURL = 'http://localhost:8080';
    const numUsers = 30000;

    console.log(`START_HOUR: ${startHour}`);
    console.log(`START_MINUTE: ${startMinute}`);

    const startTime = new Date();
    startTime.setHours(startHour, startMinute, 0, 0);

    let successCount = 0;
    let failCount = 0;

    for (let i = 0; i < numUsers; i++) {
        const userID = i + 1;
        try {
            const response = await sendPostRequest(`${baseURL}/appointment`, { userID });
            if (response && response.status === 200 && response.data.message === 'appointment scheduled successfully') {
                successCount++;
            } else {
                failCount++;
            }
        } catch (error) {
            failCount++;
        }
    }

    console.log(`Appointments - Success: ${successCount}, Fail: ${failCount}`);

    // Step 2: Wait until the specified start time plus one minute
    while (true) {
        const now = new Date();
        if (now > new Date(startTime.getTime() + 60000)) { // 1 minute after start time
            console.error('Current time is past the allowed booking time');
            return;
        }
        if (now > new Date(startTime.getTime() + 5000)) { // 5 seconds after start time
            break;
        }
        // Sleep for a short duration to avoid busy-waiting
        await new Promise(resolve => setTimeout(resolve, 1000));
    }

    // Step 3: Concurrently test /book_coupon endpoint for the same numUsers users
    let connected = 0;
    let couponSuccess = 0;

    const couponPromises = Array.from({ length: numUsers }, (_, i) => {
        const userID = i + 1;
        return sendPostRequest(`${baseURL}/book_coupon`, { userID })
            .then(response => {
                connected++;
                if (response && response.status === 200 && response.data.message === 'coupon booked successfully') {
                    couponSuccess++;
                }
            })
            .catch(() => {
                failCount++;
            });
    });

    await Promise.all(couponPromises);

    console.log(`${connected} users connected to server`);
    console.log(`${couponSuccess} users got the coupon`);
};

runTest().catch(error => {
    console.error('Test failed:', error);
});
