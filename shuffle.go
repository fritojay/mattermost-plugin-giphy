package main

import (
	"net/http"

	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/plugin"
)

// Contains what's related to the Shuffle command

// executeCommandGifShuffle returns an ephemeral (private) post with one GIF that can either be posted, shuffled or canceled
func (p *GiphyPlugin) executeCommandGifShuffle(command string, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	keywords := getCommandKeywords(command, triggerGifs)
	gifURL, err := p.gifProvider.getGifURL(p.config(), keywords)
	if err != nil {
		return nil, appError("Unable to get GIF URL", err)
	}

	text := p.generateGifCaption(keywords, gifURL)
	attachments := p.generateShufflePostAttachments(keywords, gifURL)

	return &model.CommandResponse{ResponseType: model.COMMAND_RESPONSE_TYPE_EPHEMERAL, Text: text, Attachments: attachments}, nil
}

type handlerFunc func(request *model.PostActionIntegrationRequest, keywords string, gifURL string) int

// handleHTTPAction reads the Gif context for an action (buttons) and execute the action
func (p *GiphyPlugin) handleHTTPAction(action handlerFunc, c *plugin.Context, w http.ResponseWriter, r *http.Request) {
	// Read data added by default for a button action
	request := model.PostActionIntegrationRequestFromJson(r.Body)
	if request == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	gifURL, ok := request.Context[contextGifURL]
	if !ok {
		p.logHandlerError("Missing "+contextGifURL+" from action request context", nil, request)
		w.WriteHeader(http.StatusBadRequest)
	}
	keywords, ok := request.Context[contextKeywords]
	if !ok {
		p.logHandlerError("Missing "+contextKeywords+" from action request context", nil, request)
		w.WriteHeader(http.StatusBadRequest)
	}

	httpStatus := action(request, keywords.(string), gifURL.(string))
	w.WriteHeader(httpStatus)

	if httpStatus == http.StatusOK {
		// Return the object the MM server expects in case of 200 status
		response := &model.PostActionIntegrationResponse{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response.ToJson())
	}
}

func (p *GiphyPlugin) generateShufflePostAttachments(keywords string, gifURL string) []*model.SlackAttachment {
	actionContext := map[string]interface{}{
		contextKeywords: keywords,
		contextGifURL:   gifURL,
	}

	actions := []*model.PostAction{}
	actions = append(actions, p.generateButton("Cancel", URLCancel, actionContext))
	actions = append(actions, p.generateButton("Shuffle", URLShuffle, actionContext))
	actions = append(actions, p.generateButton("Send", URLSend, actionContext))

	attachments := []*model.SlackAttachment{}
	attachments = append(attachments, &model.SlackAttachment{
		Actions: actions,
	})

	return attachments
}

// handleCancel delete the ephemeral shuffle post
func (p *GiphyPlugin) handleCancel(request *model.PostActionIntegrationRequest, keywords string, gifURL string) int {
	post := &model.Post{
		Id: request.PostId,
	}
	p.API.DeleteEphemeralPost(request.UserId, post)

	return http.StatusOK
}

// handleShuffle replace the GIF in the ephemeral shuffle post by a new one
func (p *GiphyPlugin) handleShuffle(request *model.PostActionIntegrationRequest, keywords string, gifURL string) int {
	shuffledGifURL, err := p.gifProvider.getGifURL(p.config(), keywords)
	if err != nil {
		p.logHandlerError("Unable to fetch a new Gif for shuffling", err, request)
		return http.StatusServiceUnavailable
	}

	post := &model.Post{
		Id:        request.PostId,
		ChannelId: request.ChannelId,
		UserId:    request.UserId,
		Message:   p.generateGifCaption(keywords, shuffledGifURL),
		Props: map[string]interface{}{
			"attachments": p.generateShufflePostAttachments(keywords, shuffledGifURL),
		},
		CreateAt: model.GetMillis(),
		UpdateAt: model.GetMillis(),
	}
	p.API.UpdateEphemeralPost(request.UserId, post)
	return http.StatusOK
}

// handlePost post the actual GIF and delete the obsolete ephemeral post
func (p *GiphyPlugin) handlePost(request *model.PostActionIntegrationRequest, keywords string, gifURL string) int {
	ephemeralPost := &model.Post{
		Id: request.PostId,
	}
	p.API.DeleteEphemeralPost(request.UserId, ephemeralPost)
	post := &model.Post{
		Message:   p.generateGifCaption(keywords, gifURL),
		UserId:    request.UserId,
		ChannelId: request.ChannelId,
	}
	_, err := p.API.CreatePost(post)
	if err != nil {
		p.logHandlerError("Unable to create post : ", err, request)
		return http.StatusInternalServerError
	}
	return http.StatusOK
}

// logHandlerError informs the user of an error that occured in a buttion handler, and also logs it
func (p *GiphyPlugin) logHandlerError(message string, err error, request *model.PostActionIntegrationRequest) {
	p.API.SendEphemeralPost(request.UserId, &model.Post{
		Message:   "Giphy Plugin: " + message + "\n`" + err.Error() + "`",
		ChannelId: request.ChannelId,
		Props: map[string]interface{}{
			"sent_by_plugin": true,
		},
	})
	p.API.LogWarn(message, appError("", err))
}
