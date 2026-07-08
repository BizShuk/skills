module.exports = {
    apps: [
        // Skills Auto-Update (Local)
        {
            name: "Skills Auto-Update",
            script: "skills",
            args: ["update"],
            namespace: "Local",
            instances: 1,
            cron: "0 19 * * *",
        }
    ],
};
