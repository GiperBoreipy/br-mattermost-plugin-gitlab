// Copyright (c) 2019-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import {act, render, screen} from '@testing-library/react';
import '@testing-library/jest-dom';

import RHSSidebar from './rhs_sidebar';

test('shows path filters separately and supports legacy subscriptions', async () => {
    await act(async () => {
        render(
            <RHSSidebar
                currentUserId='user'
                connected={true}
                pluginServerRoute='/plugins/gitlab'
                currentChannelSubscriptions={[
                    {repository_name: 'client/BRClient', repository_url: 'https://gitlab/client/BRClient', features: ['merges', 'path:"Assets/Editor/"'], paths: ['Assets/Editor/']},
                    {repository_name: 'client/Legacy', repository_url: 'https://gitlab/client/Legacy', features: ['merge_request_comments']},
                ]}
                actions={{getChannelSubscriptions: jest.fn().mockResolvedValue(null), sendEphemeralPost: jest.fn()}}
            />,
        );
    });
    expect(screen.getByText('Files and folders (MR)')).toBeInTheDocument();
    expect(screen.getByText('Assets/Editor/')).toBeInTheDocument();
    expect(screen.queryByText('path:"Assets/Editor/"')).not.toBeInTheDocument();
    expect(screen.getByText('merge_request_comments')).toBeInTheDocument();
});
