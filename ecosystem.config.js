module.exports = {
    apps: [
        // Daily 19:00 update of locally installed Claude skills.
        {
            name: "Skills Auto Update - Daily",
            script: "skills",
            args: ["update"],
            namespace: "Local",
            instances: 1,
            cron: "0 19 * * *",
        }
    ],
};
