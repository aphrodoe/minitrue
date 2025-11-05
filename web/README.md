# Minitrue Frontend - Quick Start Guide

## Prerequisites
- Node.js 16+ installed
- Backend server running on port 8080

## Installation

```bash
cd web
npm install
```

## Running the Frontend

```bash
npm start
```

The React app will start on `http://localhost:3000` and automatically open in your browser.

## Features

- **Modern UI**: Beautiful, responsive design with gradient backgrounds
- **Query Form**: Easy-to-use form for querying time-series data
- **Quick Time Ranges**: Buttons for "Last Hour", "Last 24 Hours", "Last Week", "All Data"
- **Real-time Results**: Displays query results with formatted JSON
- **Error Handling**: Shows error messages if queries fail
- **Loading States**: Visual feedback while queries are processing

## API Integration

The frontend automatically connects to the backend API at `http://localhost:8080/query`.

The proxy is configured in `package.json`:
```json
"proxy": "http://localhost:8080"
```

## Building for Production

```bash
npm run build
```

This creates an optimized production build in the `build/` directory.

## Project Structure

```
web/
├── public/
│   └── index.html          # HTML template
├── src/
│   ├── components/
│   │   ├── QueryForm.js     # Query input form
│   │   ├── QueryForm.css
│   │   ├── QueryResults.js  # Results display
│   │   └── QueryResults.css
│   ├── App.js               # Main app component
│   ├── App.css
│   ├── index.js             # React entry point
│   └── index.css            # Global styles
└── package.json             # Dependencies and scripts
```

