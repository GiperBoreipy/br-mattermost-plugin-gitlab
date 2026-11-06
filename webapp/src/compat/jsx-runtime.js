// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

const React = require('react');

// Always use the host's React, including on Mattermost 12 (React 19).
// createElement works on both React 17/18 and React 19 and avoids bundling
// a JSX runtime that constructs elements for a different React version.
function jsx(type, props, key) {
    const config = {...props};
    if (typeof key !== 'undefined') {
        config.key = key;
    }
    return React.createElement(type, config);
}

exports.Fragment = React.Fragment;
exports.jsx = jsx;
exports.jsxs = jsx;
exports.jsxDEV = jsx;
