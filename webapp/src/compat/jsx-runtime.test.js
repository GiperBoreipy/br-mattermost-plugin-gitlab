// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

const React = require('react');

const runtime = require('./jsx-runtime');

describe('host React JSX runtime compatibility', () => {
    test.each(['jsx', 'jsxs', 'jsxDEV'])('%s creates host React elements', (method) => {
        const props = {children: ['one', 'two'], title: 'test'};
        const element = runtime[method]('div', props, 'item');

        expect(React.isValidElement(element)).toBe(true);
        expect(element.type).toBe('div');
        expect(element.key).toBe('item');
        expect(element.props).toEqual(props);
        expect(props).not.toHaveProperty('key');
    });

    test('preserves props keys and supports fragments', () => {
        expect(runtime.Fragment).toBe(React.Fragment);
        expect(runtime.jsx(runtime.Fragment, {key: 'fragment'}).key).toBe('fragment');
    });
});
