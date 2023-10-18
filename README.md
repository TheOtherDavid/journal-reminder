# journal-reminder
Journal Reminder is a tool to automatically send daily email summaries of past journal entries. It helps rediscover meaningful moments without having to manually skim through cumbersome journal files.

Overview
This app parses Google Docs containing past journal entries. It checks for an entry from the current day in a previous year, extracts the content, and emails it as a daily journal snippet.

Getting two or three reminders a week helps me remember my past, to see how far I've come, and share memories with my friends. with previous versions of oneself and relive meaningful memories that may have faded over time.

Usage
The app is currently setup to run as an AWS Lambda triggered on a CloudWatch schedule.
It requires a service account with Google Docs access and sender credentials to enable email functionality.

To add documents for parsing:
Share the Google Doc link with the service account email
Add the document ID to the documentIDs list passed in from the CloudWatch event (or the CLI)